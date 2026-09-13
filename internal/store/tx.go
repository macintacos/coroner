package store

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"
)

const firstBackoff = 20 * time.Millisecond

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
