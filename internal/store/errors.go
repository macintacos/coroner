package store

import "errors"

var (
	// ErrForeignDatabase means the file is a SQLite database some other program wrote.
	ErrForeignDatabase = errors.New("not a coroner database")
	// ErrDatabaseTooNew means a newer coroner migrated the database past the schemas this
	// build knows.
	ErrDatabaseTooNew = errors.New("this database was written by a newer coroner")
)
