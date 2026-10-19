// Package store is coroner's SQLite store. It opens and migrates the database and maps
// its rows to and from internal/timeline types, and keeps attachment blobs in a
// content-addressed directory beside it; the sqlc-generated queries in sqlcgen never leave
// it.
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
	"syscall"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/macintacos/coroner/internal/config"
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
	blobDir    string
}

// Schema is a database's schema version, and the latest one this build embeds.
type Schema struct {
	Version, Latest int64
}

// Open opens the coroner database at path, creating it and its directory (mode 0700) when
// missing, and applies pending migrations. Before migrating a database that already has a
// schema, it copies it to <path>.pre-v<N>.bak, N being the schema it migrates to. While
// migrations are pending it holds a flock on <path>.lock, which stays beside the database
// once created. It returns ErrForeignDatabase for a directory or a SQLite file coroner did
// not create, ErrDatabaseTooNew for a database a newer coroner migrated, even while Open
// waited on the lock, and ErrBackupExists when that backup is already there.
func Open(ctx context.Context, path string) (*Store, error) {
	fsys, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}

	return open(ctx, path, fsys)
}

func embeddedMigrations() (fs.FS, error) {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}

	return fsys, nil
}

// storeAfter returns the Store over db once checkErr, the result of checking it, is nil, and
// otherwise closes db.
func storeAfter(path string, db *sql.DB, fsys fs.FS, checkErr error) (*Store, error) {
	if checkErr != nil {
		return nil, errors.Join(fmt.Errorf("open %s: %w", path, translate(checkErr)), db.Close())
	}

	return &Store{db: db, migrations: fsys, blobDir: config.BlobDir(path)}, nil
}

func tooNew(current int64) error {
	return fmt.Errorf("%w (schema %d); upgrade coroner", ErrDatabaseTooNew, current)
}

// OpenReadOnly opens the existing coroner database at path without creating, migrating or
// writing to it. SQLite may leave -shm and -wal files beside a WAL database; the database
// file itself is untouched. It returns an error wrapping fs.ErrNotExist when nothing is at
// path, and ErrForeignDatabase or ErrDatabaseTooNew as Open does. Below the latest schema,
// only Schema may be called.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	fsys, err := embeddedMigrations()
	if err != nil {
		return nil, err
	}

	// mode=ro reports a missing file as SQLITE_CANTOPEN, not fs.ErrNotExist.
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, directoryError(path)
	}

	// Not immutable=1: it would leave no -shm/-wal behind, but it ignores a live writer's WAL.
	db, err := sql.Open("sqlite", dsn(path)+"&mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	_, err = inspect(ctx, db, fsys)
	return storeAfter(path, db, fsys, err)
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

	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return nil, directoryError(path)
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	return storeAfter(path, db, fsys, prepare(ctx, db, path, fsys))
}

// directoryError is the error for a directory at the database path, which SQLite reports only
// as a failure to open.
func directoryError(path string) error {
	return fmt.Errorf("open %s: %w: it is a directory", path, ErrForeignDatabase)
}

// dsn escapes path because SQLite reads a file: URI, where ?, # and % are syntax.
// _txlock=immediate makes every write transaction BEGIN IMMEDIATE, while modernc begins a
// ReadOnly one deferred, which keeps reads off the write lock.
func dsn(path string) string {
	return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)&_txlock=immediate",
		url.PathEscape(path), busyTimeout.Milliseconds())
}

func prepare(ctx context.Context, db *sql.DB, path string, fsys fs.FS) error {
	pending, err := inspect(ctx, db, fsys)
	if err != nil {
		return err
	}

	// goose has no SQLite locker. Waiting on the lock can outlast another coroner's migration,
	// so migrate checks the version again once it is held.
	if pending {
		unlock, err := lockFile(path + ".lock")
		if err != nil {
			return err
		}
		defer unlock()
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
	if resultCode(err) == sqlite3.SQLITE_NOTADB {
		return fmt.Errorf("%w: %w", ErrForeignDatabase, err)
	}
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

// inspect refuses a database Open must not touch, and reports whether migrations in fsys are
// pending.
func inspect(ctx context.Context, db *sql.DB, fsys fs.FS) (bool, error) {
	err := refuseForeign(ctx, db)
	if err != nil {
		return false, err
	}

	return refuseTooNew(ctx, db, fsys)
}

// refuseTooNew returns ErrDatabaseTooNew for a database migrated past the migrations in fsys,
// and otherwise whether any of them are pending.
func refuseTooNew(ctx context.Context, db *sql.DB, fsys fs.FS) (bool, error) {
	latest, err := latestVersion(db, fsys)
	if err != nil {
		return false, err
	}

	current, err := schemaVersion(ctx, db)
	if err != nil {
		return false, err
	}

	if current > latest {
		return false, tooNew(current)
	}

	return current < latest, nil
}

// lockFile blocks until it holds an exclusive flock on path, creating the file when missing,
// and returns the function that releases it. The file stays: unlinking it would let a waiter
// lock a file no later process opens. syscall.Flock makes the store unix-only.
// ponytail: blocks ignoring ctx, so a hung migrating coroner silently blocks every later one;
// poll LOCK_EX|LOCK_NB against ctx if that bites.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock migrations: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("lock migrations: %w", err), f.Close())
	}

	// Closing the file releases the lock, and a failed close leaves nothing to undo.
	return func() { _ = f.Close() }, nil
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

	if current > target {
		return tooNew(current)
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
