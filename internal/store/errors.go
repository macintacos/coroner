package store

import (
	"database/sql"
	"errors"
	"fmt"

	sqlite3 "modernc.org/sqlite/lib"
)

var (
	// ErrNotFound means no record has the given id, name or seq, or no timeline is current.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a record with the same id or unique name already exists.
	ErrConflict = errors.New("already exists")
	// ErrBusy means another writer held the database for every attempt.
	ErrBusy = errors.New("database is busy")
	// ErrForeignDatabase means the file is a SQLite database some other program wrote.
	ErrForeignDatabase = errors.New("not a coroner database")
	// ErrDatabaseTooNew means a newer coroner migrated the database past the schemas this
	// build knows.
	ErrDatabaseTooNew = errors.New("this database was written by a newer coroner")
	// ErrBackupExists means the file Open backs the database up to before migrating is
	// already there.
	ErrBackupExists = errors.New("backup already exists")
)

// translate wraps a driver error in the store error a caller can branch on, and returns
// any other error as it is.
func translate(err error) error {
	code := resultCode(err)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return fmt.Errorf("%w: %w", ErrConflict, err)
	case isBusy(code):
		return fmt.Errorf("%w: %w", ErrBusy, err)
	}

	return err
}

// resultCode returns the extended SQLite result code in err's chain, or 0.
func resultCode(err error) int {
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		return coded.Code()
	}

	return 0
}

// isBusy reports whether code is SQLITE_BUSY or one of its extended codes.
func isBusy(code int) bool {
	return code&0xff == sqlite3.SQLITE_BUSY
}
