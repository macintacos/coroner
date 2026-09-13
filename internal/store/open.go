// Package store is coroner's SQLite store. It opens and migrates the database and maps
// its rows to and from internal/timeline types; the sqlc-generated queries in sqlcgen
// never leave it.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// applicationID is 'corn', stamped by the first migration.
const applicationID = 0x636f726e

// busyTimeout is how long SQLite waits for the write lock on each attempt, so a write
// fails with ErrBusy after about writeAttempts × busyTimeout.
const busyTimeout = 5 * time.Second

//go:embed migrations/*.sql
var migrations embed.FS

// Store is an open coroner database. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open opens the coroner database at path, creating it and its directory (mode 0700) when
// missing, and applies pending migrations. Before migrating a database that already has a
// schema, it copies it to <path>.pre-v<N>.bak, N being the schema it migrates to. It
// returns ErrForeignDatabase for a SQLite file coroner did not create, ErrDatabaseTooNew
// for one a newer coroner migrated, and ErrBackupExists when that backup is already there.
func Open(ctx context.Context, path string) (*Store, error) {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}

	return open(ctx, path, fsys)
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func open(ctx context.Context, path string, fsys fs.FS) (*Store, error) {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	err = prepare(ctx, db, path, fsys)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open %s: %w", path, translate(err)), db.Close())
	}

	return &Store{db: db}, nil
}

// dsn escapes path because SQLite reads a file: URI, where ?, # and % are syntax.
// _txlock=immediate makes every write transaction BEGIN IMMEDIATE, while modernc begins a
// ReadOnly one deferred, which keeps reads off the write lock.
func dsn(path string) string {
	return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)&_txlock=immediate",
		url.PathEscape(path), busyTimeout.Milliseconds())
}

func prepare(ctx context.Context, db *sql.DB, path string, fsys fs.FS) error {
	err := refuseForeign(ctx, db)
	if err != nil {
		return err
	}

	// Not a DSN pragma: that runs on connect and would rewrite a foreign file's header
	// before refuseForeign could refuse it. WAL mode persists in the file, so once is enough.
	_, err = db.ExecContext(ctx, "PRAGMA journal_mode = WAL")
	if err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}

	return migrate(ctx, db, path, fsys)
}

// refuseForeign returns ErrForeignDatabase for a database coroner did not create.
func refuseForeign(ctx context.Context, db *sql.DB) error {
	var id, others int64
	err := db.QueryRowContext(ctx, `SELECT
		(SELECT application_id FROM pragma_application_id()),
		(SELECT count(*) FROM sqlite_schema WHERE name NOT IN (?, 'sqlite_sequence'))`,
		goose.DefaultTablename,
	).Scan(&id, &others)
	if err != nil {
		return fmt.Errorf("read database identity: %w", err)
	}

	// Id 0 with only goose's own objects is coroner's first migration that failed after
	// goose created its table: the application id rolled back with the migration.
	if (id != applicationID && id != 0) || (id == 0 && others > 0) {
		return ErrForeignDatabase
	}

	return nil
}

func migrate(ctx context.Context, db *sql.DB, path string, fsys fs.FS) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	current, target, err := provider.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if current > target {
		return fmt.Errorf("%w (schema %d); upgrade coroner", ErrDatabaseTooNew, current)
	}
	if current > 0 && current < target {
		err = backUp(ctx, db, fmt.Sprintf("%s.pre-v%d.bak", path, target))
		if err != nil {
			return err
		}
	}

	_, err = provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

// backUp copies the database to a new file at backup and never overwrites one.
func backUp(ctx context.Context, db *sql.DB, backup string) error {
	_, err := os.Lstat(backup)
	if err == nil {
		return fmt.Errorf("back up before migrating: %w: %s; move it aside and retry", ErrBackupExists, backup)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("back up before migrating: %w", err)
	}

	_, err = db.ExecContext(ctx, "VACUUM INTO ?", backup)
	if err != nil {
		return fmt.Errorf("back up before migrating to %s: %w", backup, err)
	}

	return nil
}
