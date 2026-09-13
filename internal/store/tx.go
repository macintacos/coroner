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

// read runs fn in a read-only transaction, which never takes the write lock.
func (s *Store) read(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
	return translate(s.inTx(ctx, &sql.TxOptions{ReadOnly: true}, fn))
}

// write runs fn in one immediate transaction, retried whole while another writer holds the
// database. fn may therefore run more than once, so it must change nothing outside the
// transaction.
func (s *Store) write(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
	return translate(retryBusy(ctx, writeAttempts, func() error {
		return s.inTx(ctx, nil, fn)
	}))
}

func (s *Store) inTx(ctx context.Context, opts *sql.TxOptions, fn func(*sqlcgen.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}

	err = fn(sqlcgen.New(tx))
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
