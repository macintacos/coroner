package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
)

const (
	writeAttempts = 3
	firstBackoff  = 20 * time.Millisecond
)

// read runs fn in a transaction modernc begins deferred, so it waits on no writer, and
// returns what fn produced. Nothing stops fn from writing; use write for that. fn must only
// use q: another Store method would run outside this transaction.
func read[T any](ctx context.Context, s *Store, fn func(*sqlcgen.Queries) (T, error)) (T, error) {
	return readTx(ctx, s, func(tx *sql.Tx) (T, error) { return fn(sqlcgen.New(tx)) })
}

// readTx is read for hand-written SQL: fn must only use tx.
func readTx[T any](ctx context.Context, s *Store, fn func(*sql.Tx) (T, error)) (T, error) {
	var result T
	err := s.inTx(ctx, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
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

// write runs fn in one immediate transaction, retried whole while another writer holds the
// database, so fn may run more than once and must only use q: calling another Store method
// from fn waits on this transaction's own lock until ErrBusy.
func (s *Store) write(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
	return translate(retryBusy(ctx, writeAttempts, func() error {
		return s.inTx(ctx, nil, func(tx *sql.Tx) error { return fn(sqlcgen.New(tx)) })
	}))
}

func (s *Store) inTx(ctx context.Context, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
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
