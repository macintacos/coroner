package store

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"time"
)

func (s *Store) DB() *sql.DB { return s.db }

func OpenWithMigrations(ctx context.Context, path string, migrations fs.FS) (*Store, error) {
	opts, err := defaultOpenOptions()
	if err != nil {
		return nil, err
	}
	opts.migrations = migrations

	return open(ctx, path, opts)
}

func OpenWithBusyTimeout(ctx context.Context, path string, busyTimeout time.Duration) (*Store, error) {
	opts, err := defaultOpenOptions()
	if err != nil {
		return nil, err
	}
	opts.busyTimeout = busyTimeout

	return open(ctx, path, opts)
}

func OpenWithBlobSync(ctx context.Context, path string, sync func(*os.File) error) (*Store, error) {
	opts, err := defaultOpenOptions()
	if err != nil {
		return nil, err
	}
	opts.syncBlob = sync

	return open(ctx, path, opts)
}

func BuildEventQuery(f EventFilter) (string, []any) {
	return buildEventQuery(f)
}
