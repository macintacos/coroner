package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// codedError carries a SQLite result code the way the driver's *sqlite.Error does.
type codedError int

func (e codedError) Error() string { return fmt.Sprintf("sqlite result code %d", int(e)) }

func (e codedError) Code() int { return int(e) }

const (
	errBusy             codedError = 5
	errBusySnapshot     codedError = 517
	errUniqueFailed     codedError = 2067
	errPrimaryKeyFailed codedError = 1555
	errCheckFailed      codedError = 275
)

var _ = Describe("retryBusy", func() {
	It("gives up after the last attempt, returning the busy error", func(ctx SpecContext) {
		calls := 0

		err := retryBusy(ctx, 3, func() error {
			calls++
			return errBusy
		})

		Expect(err).To(MatchError(errBusy))
		Expect(calls).To(Equal(3))
	})

	It("retries an extended busy code", func(ctx SpecContext) {
		calls := 0

		err := retryBusy(ctx, 2, func() error {
			calls++
			if calls == 1 {
				return fmt.Errorf("begin: %w", errBusySnapshot)
			}
			return nil
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(2))
	})

	It("returns any other error without retrying", func(ctx SpecContext) {
		calls := 0

		err := retryBusy(ctx, 3, func() error {
			calls++
			return errCheckFailed
		})

		Expect(err).To(MatchError(errCheckFailed))
		Expect(calls).To(Equal(1))
	})

	It("stops with the context's error when the context ends before a retry", func(spec SpecContext) {
		ctx, cancel := context.WithCancel(spec)
		calls := 0

		err := retryBusy(ctx, 3, func() error {
			calls++
			cancel()
			return errBusy
		})

		Expect(err).To(MatchError(context.Canceled))
		Expect(calls).To(Equal(1))
	})
})

var _ = DescribeTable("translate types a driver error, keeping it in the chain",
	func(err, want error) {
		Expect(translate(err)).To(And(MatchError(want), MatchError(err)))
	},
	Entry("no rows as ErrNotFound", fmt.Errorf("get timeline: %w", sql.ErrNoRows), ErrNotFound),
	Entry("a UNIQUE violation as ErrConflict", errUniqueFailed, ErrConflict),
	Entry("a PRIMARY KEY violation as ErrConflict", errPrimaryKeyFailed, ErrConflict),
	Entry("busy as ErrBusy", errBusy, ErrBusy),
	Entry("an extended busy code as ErrBusy", errBusySnapshot, ErrBusy),
)

var _ = Describe("translate", func() {
	It("returns any other error as it is", func() {
		Expect(translate(errCheckFailed)).To(Equal(error(errCheckFailed)))
		Expect(translate(errors.ErrUnsupported)).To(Equal(errors.ErrUnsupported))
	})

	It("returns nil for nil", func() {
		Expect(translate(nil)).To(Succeed())
	})
})

var _ = DescribeTable("translate returns an error it already typed as it is",
	func(err error) {
		translated := translate(err)
		Expect(translate(translated)).To(Equal(translated))
	},
	Entry("an already translated ErrNotFound", sql.ErrNoRows),
	Entry("an already translated ErrConflict", errUniqueFailed),
	Entry("an already translated ErrBusy", errBusy),
)
