package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"math/big"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	writeAttempts = 3
	firstBackoff  = 20 * time.Millisecond
)

// Mode says whether a Change commits or rolls back.
type Mode int

const (
	// Commit keeps what the change wrote.
	Commit Mode = iota
	// Rollback runs the change in full, then undoes it: a dry run. It names no blob.
	Rollback
)

// ChangeOptions says how a Change ends and what content it may attach or import.
type ChangeOptions struct {
	Mode Mode
	// Attach is content staged before the transaction begins, because a reader cannot be read
	// twice. fn attaches it to one owner with Tx.Attach; content fn never attaches is discarded.
	Attach []NewAttachment
	// Blobs is content staged before the transaction begins, with no owner. Tx.Import claims the
	// one whose SHA256 an attachment it inserts carries; Change names those after fn and discards
	// the rest.
	Blobs []io.Reader
}

// Tx is one transaction's view of the database. Its methods translate their own errors, and a
// write method that fails leaves nothing behind, so a caller can branch on ErrNotFound and
// ErrConflict inside a Change and keep going. A Tx is valid only while the function it was
// passed to runs, and is not safe for concurrent use.
type Tx struct {
	q       *sqlcgen.Queries
	db      sqlcgen.DBTX // for SQL sqlc did not generate
	blobDir string
	owner   *timeline.AttachmentOwner // the owner Attach named; nil attaches nothing
	blobs   map[string]stagedBlob     // the Change's Blobs, by SHA256
	claimed []stagedBlob              // the blobs Tx.Import claimed
}

func newTx(db sqlcgen.DBTX, blobDir string) *Tx {
	return &Tx{q: sqlcgen.New(db), db: db, blobDir: blobDir}
}

// Attach attaches the Change's staged content to owner when fn returns. Only Store.Change
// attaches; in Read it does nothing. A second call replaces the owner.
func (tx *Tx) Attach(owner timeline.AttachmentOwner) {
	tx.owner = &owner
}

// Change runs fn in one immediate transaction, then names the blobs Tx.Import claimed, and
// names and inserts the content fn attached, then commits, or rolls back under Rollback. fn may run
// more than once and must use only tx. It returns the attachment rows it attached (none under
// Rollback).
func (s *Store) Change(ctx context.Context, opts ChangeOptions, fn func(*Tx) error) ([]timeline.Attachment, error) {
	err := validateEach(opts.Attach)
	if err != nil {
		return nil, err
	}

	staged, err := stageAttachments(s.blobDir, opts.Attach)
	if err != nil {
		return nil, err
	}
	defer discardStaged(staged)

	blobs, err := stageBlobs(s.blobDir, opts.Blobs)
	if err != nil {
		return nil, err
	}
	defer discardBlobs(blobs)

	var inserted []stagedAttachment
	err = s.change(ctx, opts.Mode, func(tx *Tx) error {
		inserted = nil
		tx.blobs = bySHA256(blobs)
		err := fn(tx)
		if err != nil || opts.Mode == Rollback {
			return err
		}
		for _, b := range tx.claimed {
			err = commitBlob(tx.blobDir, b)
			if err != nil {
				return err
			}
		}
		if tx.owner == nil {
			return nil
		}
		inserted = owned(staged, *tx.owner)
		return insertAttachments(ctx, tx.q, tx.blobDir, inserted)
	})
	if err != nil {
		return nil, err
	}

	var rows []timeline.Attachment
	for _, a := range inserted {
		rows = append(rows, a.attachment)
	}

	return rows, nil
}

// Read runs fn in one deferred transaction, which takes no write lock. fn must only read:
// Read does not stop it writing. fn must use only tx.
func (s *Store) Read(ctx context.Context, fn func(*Tx) error) error {
	_, err := readIn(ctx, s, func(tx *Tx) (struct{}, error) { return struct{}{}, fn(tx) })
	return err
}

// readIn runs fn in one read transaction and returns what it produced. fn must only use tx.
func readIn[T any](ctx context.Context, s *Store, fn func(*Tx) (T, error)) (T, error) {
	return readTx(ctx, s, func(db sqlcgen.DBTX) (T, error) { return fn(newTx(db, s.blobDir)) })
}

// translated runs fn and returns its result, or the zero value and its error translated.
func translated[T any](fn func() (T, error)) (T, error) {
	v, err := fn()
	if err != nil {
		var zero T
		return zero, translate(err)
	}

	return v, nil
}

// readTx runs fn in a transaction modernc begins deferred, so it waits on no writer, and
// returns what fn produced. Nothing stops fn from writing; use change for that. fn must only
// use tx: another Store method would run outside this transaction.
func readTx[T any](ctx context.Context, s *Store, fn func(sqlcgen.DBTX) (T, error)) (T, error) {
	var result T
	err := s.inTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx sqlcgen.DBTX) error {
		var err error
		result, err = fn(tx)
		return err
	})
	if err != nil {
		var zero T
		return zero, translate(err)
	}

	return result, nil
}

// loadAll maps rows with load, stopping at the first error.
// ponytail: per-row queries are cheap in-process; a joined or group_concat query if a
// large timeline reads slowly.
func loadAll[R, T any](rows []R, load func(R) (T, error)) ([]T, error) {
	loaded := make([]T, 0, len(rows))
	for _, row := range rows {
		t, err := load(row)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, t)
	}

	return loaded, nil
}

// queryAll runs hand-written SQL on tx and maps each row with scan, stopping at the first
// error. It never returns a nil slice with a nil error.
func queryAll[T any](
	ctx context.Context, tx sqlcgen.DBTX, query string, args []any, scan func(*sql.Rows) (T, error),
) ([]T, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	// rows.Err reports what went wrong; Close only matters on an early return.
	defer func() { _ = rows.Close() }()

	scanned := []T{}
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		scanned = append(scanned, t)
	}

	return scanned, rows.Err()
}

// atomic runs fn inside a savepoint, so a failed fn leaves nothing behind in the transaction,
// and returns fn's error translated.
func (tx *Tx) atomic(ctx context.Context, fn func() error) error {
	_, err := tx.db.ExecContext(ctx, "SAVEPOINT w")
	if err != nil {
		return translate(err)
	}

	err = fn()
	if err != nil {
		_, rollbackErr := tx.db.ExecContext(ctx, "ROLLBACK TO w")
		_, releaseErr := tx.db.ExecContext(ctx, "RELEASE w")
		return translate(errors.Join(err, rollbackErr, releaseErr))
	}

	_, err = tx.db.ExecContext(ctx, "RELEASE w")
	return translate(err)
}

// errRollback ends a Rollback change's transaction; change reports it as success.
var errRollback = errors.New("rolled back")

// change runs fn in one immediate transaction retried whole while another writer holds the
// database. Under Rollback it undoes fn's writes. fn may run more than once and must only use
// tx.
func (s *Store) change(ctx context.Context, mode Mode, fn func(*Tx) error) error {
	err := retryBusy(ctx, writeAttempts, func() error {
		return s.inTx(ctx, nil, func(db sqlcgen.DBTX) error {
			tx := newTx(db, s.blobDir)
			err := fn(tx)
			if err != nil {
				return err
			}
			if mode == Rollback {
				return errRollback
			}

			return nil
		})
	})
	// A failed ROLLBACK joined to errRollback still committed nothing.
	if errors.Is(err, errRollback) {
		return nil
	}

	return translate(err)
}

func (s *Store) inTx(ctx context.Context, opts *sql.TxOptions, fn func(sqlcgen.DBTX) error) error {
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}

	err = fn(tx)
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}

	return tx.Commit()
}

// retryBusy calls fn up to attempts times while it fails with SQLITE_BUSY, waiting a
// jittered, doubling backoff between calls, and returns its last error.
func retryBusy(ctx context.Context, attempts int, fn func() error) error {
	backoff := firstBackoff
	for attempt := 1; ; attempt++ {
		err := fn()
		if attempt == attempts || !isBusy(resultCode(err)) {
			return err
		}
		// select picks at random when the timer is also ready, so check first.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(backoff)):
		}
		backoff *= 2
	}
}

// jitter returns a random duration in [0, d), so writers that collided retry apart.
func jitter(d time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(d)))
	if err != nil {
		return d
	}

	return time.Duration(n.Int64())
}
