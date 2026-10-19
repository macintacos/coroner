package cli

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("validation", func() {
	DescribeTable("kindOf maps every rule to its exit kind",
		func(r timeline.Rule, want Kind) {
			Expect(kindOf(r)).To(Equal(want))
		},
		Entry(nil, timeline.RuleBlank, KindUsage),
		Entry(nil, timeline.RuleNotUTF8, KindUsage),
		Entry(nil, timeline.RuleGrammar, KindInvalidName),
		Entry(nil, timeline.RuleReserved, KindInvalidName),
		Entry(nil, timeline.RuleUnknown, KindUsage),
		Entry(nil, timeline.RuleYearRange, KindInvalidTime),
		Entry(nil, timeline.RulePrecisionStart, KindUsage),
		Entry(nil, timeline.RuleEndedBeforeOccurred, KindInvalidTime),
		Entry(nil, timeline.RuleOffsetRange, KindInvalidTime),
		Entry(nil, timeline.RuleSelfLink, KindUsage),
		Entry(nil, timeline.RuleOwner, KindUsage),
		Entry(nil, timeline.RuleSHA256, KindUsage),
		Entry(nil, timeline.RuleNegativeSize, KindUsage),
	)

	Describe("recordError", func() {
		blankAndOffset := &timeline.ValidationError{Problems: []timeline.Problem{
			{Field: "text", Rule: timeline.RuleBlank, Message: "text: must not be blank"},
			{Field: "occurred_offset", Rule: timeline.RuleOffsetRange, Message: "out of range"},
		}}

		It("passes nil and an *Error through unchanged", func() {
			Expect(recordError(nil, "hint")).To(Succeed())
			cliErr := &Error{Kind: KindDBBusy, Message: "busy"}
			Expect(recordError(cliErr, "hint")).To(BeIdenticalTo(cliErr))
		})

		It("passes any other error through unchanged", func() {
			plain := errors.New("plain")
			Expect(recordError(plain, "hint")).To(BeIdenticalTo(plain))
		})

		It("takes the first problem's kind and keeps every problem's message", func() {
			err, ok := errors.AsType[*Error](recordError(blankAndOffset, "hint"))
			Expect(ok).To(BeTrue())
			Expect(err.Kind).To(Equal(KindUsage))
			Expect(err.Message).To(Equal(blankAndOffset.Error()))
			Expect(err.Err).To(BeIdenticalTo(blankAndOffset))
		})

		It("adds the hint only on a usage problem", func() {
			usage, _ := errors.AsType[*Error](recordError(blankAndOffset, "hint"))
			Expect(usage.Hint).To(Equal("hint"))

			offsetOnly := &timeline.ValidationError{Problems: []timeline.Problem{
				{Field: "occurred_offset", Rule: timeline.RuleOffsetRange, Message: "out of range"},
			}}
			timeErr, _ := errors.AsType[*Error](recordError(offsetOnly, "hint"))
			Expect(timeErr.Kind).To(Equal(KindInvalidTime))
			Expect(timeErr.Hint).To(BeEmpty())
		})
	})

	It("storeError returns a store-side validation error as its first rule's kind", func() {
		offsetOnly := &timeline.ValidationError{Problems: []timeline.Problem{
			{Field: "occurred_offset", Rule: timeline.RuleOffsetRange, Message: "out of range"},
		}}

		err, ok := errors.AsType[*Error](storeError(offsetOnly, "coroner.db"))

		Expect(ok).To(BeTrue())
		Expect(err.Kind).To(Equal(KindInvalidTime))
		Expect(err.Message).To(Equal("out of range"))
	})
})
