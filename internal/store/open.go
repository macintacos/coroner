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
	"log/slog"
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
	blobDir    blobDir
	logger     *slog.Logger
	now        func() time.Time
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
func Open(ctx context.Context, path string, options ...Option) (*Store, error) {
	opts, err := defaultOpenOptions(options...)
	if err != nil {
		return nil, err
	}

	return open(ctx, path, opts)
}

// OpenWithoutBlobSync is Open without syncing a stored blob or the blob directory, which on
// macOS takes milliseconds each. A power loss can then lose a committed attachment's blob, so
// only tests, which never crash the machine, may call it.
func OpenWithoutBlobSync(ctx context.Context, path string, options ...Option) (*Store, error) {
	opts, err := defaultOpenOptions(options...)
	if err != nil {
		return nil, err
	}
	opts.syncBlob = func(*os.File) error { return nil }

	return open(ctx, path, opts)
}

// openOptions is what open varies between Open and the openers tests use.
type openOptions struct {
	migrations  fs.FS
	busyTimeout time.Duration
	syncBlob    func(*os.File) error
	logger      *slog.Logger
	now         func() time.Time
}

// Option changes how Open, OpenWithoutBlobSync or OpenReadOnly opens a database.
type Option func(*openOptions)

// WithLogger sends the store's diagnostic lines to logger. Without it they are discarded.
func WithLogger(logger *slog.Logger) Option {
	return func(o *openOptions) { o.logger = logger }
}

// WithClock times the store's steps by now. Without it every duration logs as zero.
func WithClock(now func() time.Time) Option {
	return func(o *openOptions) { o.now = now }
}

func defaultOpenOptions(options ...Option) (openOptions, error) {
	fsys, err := embeddedMigrations()
	if err != nil {
		return openOptions{}, err
	}

	opts := openOptions{
		migrations: fsys, busyTimeout: busyTimeout, syncBlob: (*os.File).Sync, logger: slog.New(slog.DiscardHandler),
		now: func() time.Time { return time.Time{} },
	}
	for _, option := range options {
		option(&opts)
	}

	return opts, nil
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
func storeAfter(path string, db *sql.DB, opts openOptions, checkErr error) (*Store, error) {
	if checkErr != nil {
		return nil, errors.Join(fmt.Errorf("open %s: %w", path, translate(checkErr)), db.Close())
	}

	return &Store{
		db:         db,
		migrations: opts.migrations,
		blobDir:    blobDir{path: config.BlobDir(path), sync: opts.syncBlob},
		logger:     opts.logger,
		now:        opts.now,
	}, nil
}

func tooNew(current int64) error {
	return fmt.Errorf("%w (schema %d); upgrade coroner", ErrDatabaseTooNew, current)
}

// OpenReadOnly opens the existing coroner database at path without creating, migrating or
// writing to it. SQLite may leave -shm and -wal files beside a WAL database; the database
// file itself is untouched. It returns an error wrapping fs.ErrNotExist when nothing is at
// path, and ErrForeignDatabase or ErrDatabaseTooNew as Open does. Below the latest schema,
// only Schema may be called.
func OpenReadOnly(ctx context.Context, path string, options ...Option) (*Store, error) {
	opts, err := defaultOpenOptions(options...)
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
	db, err := sql.Open("sqlite", dsn(path, opts.busyTimeout)+"&mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	schema, err := inspect(ctx, db, opts.migrations)
	if err == nil {
		opts.logger.DebugContext(ctx, "database opened", "path", path, "created", false, "schema_version", schema.Version)
	}
	return storeAfter(path, db, opts, err)
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

func open(ctx context.Context, path string, opts openOptions) (*Store, error) {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return nil, directoryError(path)
	}
	created := errors.Is(err, fs.ErrNotExist)

	db, err := sql.Open("sqlite", dsn(path, opts.busyTimeout))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	version, err := prepare(ctx, db, path, opts)
	if err == nil {
		opts.logger.DebugContext(ctx, "database opened", "path", path, "created", created, "schema_version", version)
	}
	return storeAfter(path, db, opts, err)
}

// directoryError is the error for a directory at the database path, which SQLite reports only
// as a failure to open.
func directoryError(path string) error {
	return fmt.Errorf("open %s: %w: it is a directory", path, ErrForeignDatabase)
}

// dsn escapes path because SQLite reads a file: URI, where ?, # and % are syntax.
// _txlock=immediate makes every write transaction BEGIN IMMEDIATE, while modernc begins a
// ReadOnly one deferred, which keeps reads off the write lock.
func dsn(path string, busy time.Duration) string {
	return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)&_txlock=immediate",
		url.PathEscape(path), busy.Milliseconds())
}

// prepare readies the database for Open and returns the schema version it is then at.
func prepare(ctx context.Context, db *sql.DB, path string, opts openOptions) (int64, error) {
	schema, err := inspect(ctx, db, opts.migrations)
	if err != nil {
		return 0, err
	}

	// goose has no SQLite locker. Waiting on the lock can outlast another coroner's migration,
	// so migrate checks the version again once it is held.
	if schema.Version < schema.Latest {
		unlock, err := lockFile(path + ".lock")
		if err != nil {
			return 0, err
		}
		defer unlock()
	}

	// Not a DSN pragma: that runs on connect and would rewrite a foreign file's header
	// before refuseForeign could refuse it. WAL mode persists in the file, so once is enough.
	_, err = db.ExecContext(ctx, "PRAGMA journal_mode = WAL")
	if err != nil {
		return 0, fmt.Errorf("enable WAL: %w", err)
	}

	err = migrate(ctx, db, path, opts)
	if err != nil {
		return 0, err
	}

	return schema.Latest, nil
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

// inspect refuses a database Open must not touch, and returns its schema against the
// migrations in fsys.
func inspect(ctx context.Context, db *sql.DB, fsys fs.FS) (Schema, error) {
	err := refuseForeign(ctx, db)
	if err != nil {
		return Schema{}, err
	}

	return refuseTooNew(ctx, db, fsys)
}

// refuseTooNew returns ErrDatabaseTooNew for a database migrated past the migrations in fsys,
// and otherwise its schema against them.
func refuseTooNew(ctx context.Context, db *sql.DB, fsys fs.FS) (Schema, error) {
	latest, err := latestVersion(db, fsys)
	if err != nil {
		return Schema{}, err
	}

	current, err := schemaVersion(ctx, db)
	if err != nil {
		return Schema{}, err
	}

	if current > latest {
		return Schema{}, tooNew(current)
	}

	return Schema{Version: current, Latest: latest}, nil
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

func migrate(ctx context.Context, db *sql.DB, path string, opts openOptions) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, opts.migrations)
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
		backup := fmt.Sprintf("%s.pre-v%d.bak", path, target)
		size, err := backUp(ctx, db, backup)
		if err != nil {
			return err
		}
		opts.logger.InfoContext(ctx, "database backed up", "path", backup, "size", size)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		opts.logger.InfoContext(ctx, "migration applied", "schema_version", r.Source.Version)
	}

	return nil
}

// backUp copies the database to a new file at backup, never overwriting one, and returns
// the copy's size.
func backUp(ctx context.Context, db *sql.DB, backup string) (int64, error) {
	_, err := os.Lstat(backup)
	if err == nil {
		return 0, fmt.Errorf("back up before migrating: %w: %s; move it aside and retry", ErrBackupExists, backup)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("back up before migrating: %w", err)
	}

	_, err = db.ExecContext(ctx, "VACUUM INTO ?", backup)
	if err != nil {
		return 0, fmt.Errorf("back up before migrating to %s: %w", backup, err)
	}

	info, err := os.Stat(backup)
	if err != nil {
		return 0, fmt.Errorf("back up before migrating: %w", err)
	}

	return info.Size(), nil
}
