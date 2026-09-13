package store

import (
	"context"
	"database/sql"
	"io/fs"
)

func (s *Store) DB() *sql.DB { return s.db }

func OpenWithMigrations(ctx context.Context, path string, migrations fs.FS) (*Store, error) {
	return open(ctx, path, migrations)
}
