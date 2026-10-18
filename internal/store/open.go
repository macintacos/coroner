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

	"github.com/macintacos/coroner/internal/store/sqlcgen"
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
	db         *sql.DB
	migrations fs.FS
}

// Schema is a database's schema version, and the latest one this build embeds.
type Schema struct {
	Version, Latest int64
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

// OpenReadOnly opens the existing coroner database at path without creating, migrating or
// writing to it. SQLite may leave -shm and -wal files beside a WAL database; the database
// file itself is untouched. It returns an error wrapping fs.ErrNotExist when nothing is at
// path, and ErrForeignDatabase or ErrDatabaseTooNew as Open does. Below the latest schema,
// only Schema may be called.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}

	// mode=ro reports a missing file as SQLITE_CANTOPEN, not fs.ErrNotExist.
	_, err = os.Stat(path)
	if err != nil {
		return nil, err
	}

	// Not immutable=1: it would leave no -shm/-wal behind, but it ignores a live writer's WAL.
	db, err := sql.Open("sqlite", dsn(path)+"&mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	err = inspect(ctx, db, fsys)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open %s: %w", path, translate(err)), db.Close())
	}

	return &Store{db: db, migrations: fsys}, nil
}

// Schema reads the database's schema version. It is safe to call below Latest; every other
// read expects the database at Latest.
func (s *Store) Schema(ctx context.Context) (Schema, error) {
	latest, err := latestVersion(s.db, s.migrations)
	if err != nil {
		return Schema{}, err
	}

	version, err := readTx(ctx, s, func(tx sqlcgen.DBTX) (int64, error) { return schemaVersion(ctx, tx) })
	if err != nil {
		return Schema{}, err
	}

	return Schema{Version: version, Latest: latest}, nil
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

	return &Store{db: db, migrations: fsys}, nil
}

// dsn escapes path because SQLite reads a file: URI, where ?, # and % are syntax.
// _txlock=immediate makes every write transaction BEGIN IMMEDIATE, while modernc begins a
// ReadOnly one deferred, which keeps reads off the write lock.
func dsn(path string) string {
	return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)&_txlock=immediate",
		url.PathEscape(path), busyTimeout.Milliseconds())
}

func prepare(ctx context.Context, db *sql.DB, path string, fsys fs.FS) error {
	err := inspect(ctx, db, fsys)
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
	var appID, otherObjects int64
	err := db.QueryRowContext(ctx, `SELECT
		(SELECT application_id FROM pragma_application_id()),
		(SELECT count(*) FROM sqlite_schema WHERE name NOT IN (?, 'sqlite_sequence'))`,
		goose.DefaultTablename,
	).Scan(&appID, &otherObjects)
	if err != nil {
		return fmt.Errorf("read database identity: %w", err)
	}

	// Id 0 with only goose's own objects is coroner's first migration that failed after
	// goose created its table: the application id rolled back with the migration.
	if (appID != applicationID && appID != 0) || (appID == 0 && otherObjects > 0) {
		return ErrForeignDatabase
	}

	return nil
}

func inspect(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	err := refuseForeign(ctx, db)
	if err != nil {
		return err
	}

	return refuseTooNew(ctx, db, fsys)
}

// refuseTooNew returns ErrDatabaseTooNew for a database migrated past the migrations in fsys.
func refuseTooNew(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	latest, err := latestVersion(db, fsys)
	if err != nil {
		return err
	}

	current, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}

	if current > latest {
		return fmt.Errorf("%w (schema %d); upgrade coroner", ErrDatabaseTooNew, current)
	}

	return nil
}

// latestVersion returns the version of the last migration in fsys.
func latestVersion(db *sql.DB, fsys fs.FS) (int64, error) {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return 0, fmt.Errorf("create migration provider: %w", err)
	}

	sources := provider.ListSources()
	return sources[len(sources)-1].Version, nil
}

// schemaVersion reads goose's table by hand: goose's GetVersions creates the table when it
// is missing, which a read-only connection refuses.
func schemaVersion(ctx context.Context, db sqlcgen.DBTX) (int64, error) {
	var tables int64
	err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?",
		goose.DefaultTablename).Scan(&tables)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if tables == 0 {
		return 0, nil
	}

	var version int64
	err = db.QueryRowContext(ctx, "SELECT coalesce(max(version_id), 0) FROM goose_db_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}

	return version, nil
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
