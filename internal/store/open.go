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

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// applicationID is 'corn', stamped by the first migration.
const applicationID = 0x636f726e

//go:embed migrations/*.sql
var migrations embed.FS

// Store is an open coroner database. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open opens the coroner database at path, creating it and its directory when missing,
// and applies pending migrations after backing the database up.
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
		return nil, errors.Join(fmt.Errorf("open %s: %w", path, err), db.Close())
	}

	return &Store{db: db}, nil
}

// dsn escapes path because SQLite reads a file: URI, where ?, # and % are syntax.
func dsn(path string) string {
	return "file:" + url.PathEscape(path) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
}

func prepare(ctx context.Context, db *sql.DB, path string, fsys fs.FS) error {
	versioned, err := claim(ctx, db)
	if err != nil {
		return err
	}

	// Not a DSN pragma: that runs on connect and would rewrite a foreign file's header
	// before claim could refuse it. WAL mode persists in the file, so once is enough.
	_, err = db.ExecContext(ctx, "PRAGMA journal_mode = WAL")
	if err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}

	return migrate(ctx, db, path, fsys, versioned)
}

// claim refuses a database coroner did not create and reports whether goose has
// versioned it.
func claim(ctx context.Context, db *sql.DB) (bool, error) {
	var id, objects, versioned int64
	err := db.QueryRowContext(ctx, `SELECT
		(SELECT application_id FROM pragma_application_id()),
		(SELECT count(*) FROM sqlite_schema),
		(SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?)`,
		goose.DefaultTablename,
	).Scan(&id, &objects, &versioned)
	if err != nil {
		return false, fmt.Errorf("read database identity: %w", err)
	}

	if (id != applicationID && id != 0) || (id == 0 && objects > 0 && versioned == 0) {
		return false, ErrForeignDatabase
	}

	return versioned > 0, nil
}

func migrate(ctx context.Context, db *sql.DB, path string, fsys fs.FS, versioned bool) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	var current int64
	if versioned {
		current, err = provider.GetDBVersion(ctx)
		if err != nil {
			return fmt.Errorf("read schema version: %w", err)
		}
	}
	sources := provider.ListSources()
	target := sources[len(sources)-1].Version

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
		return fmt.Errorf("back up before migrating: %s already exists; move it aside and retry", backup)
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
