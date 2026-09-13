// Package db is coroner's SQLite store: sqlc-generated queries plus goose migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the SQLite database at path and applies pending migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", "file:"+path+
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	err = migrate(ctx, conn)
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}

	return conn, nil
}

func migrate(ctx context.Context, conn *sql.DB) error {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, conn, fsys)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	_, err = provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
