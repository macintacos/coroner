package timeline_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Gaps", func() {
	at := func(hour, minute, second int) time.Time {
		return time.Date(2026, 9, 11, hour, minute, second, 0, time.UTC)
	}
	moment := func(occurred time.Time) timeline.Event {
		return timeline.Event{OccurredAt: occurred, Precision: timeline.PrecisionSecond}
	}
	interval := func(occurred, ended time.Time) timeline.Event {
		e := moment(occurred)
		e.EndedAt = &ended
		return e
	}

	DescribeTable("finds the stretches of at least the threshold that no event covers",
		func(events []timeline.Event, want []timeline.Gap) {
			Expect(timeline.Gaps(events, 5*time.Minute)).To(Equal(want))
		},
		Entry("none without events", nil, nil),
		Entry("none with one event", []timeline.Event{moment(at(3, 0, 0))}, nil),
		Entry("none just below the threshold",
			[]timeline.Event{moment(at(3, 0, 0)), moment(at(3, 4, 59))}, nil),
		Entry("one at exactly the threshold, before the later event",
			[]timeline.Event{moment(at(3, 0, 0)), moment(at(3, 5, 0))},
			[]timeline.Gap{{Before: 1, Duration: 5 * time.Minute}}),
		Entry("none between events at the same instant",
			[]timeline.Event{moment(at(3, 0, 0)), moment(at(3, 0, 0))}, nil),
		Entry("none while an interval covers the silence",
			[]timeline.Event{interval(at(3, 0, 0), at(3, 30, 0)), moment(at(3, 20, 0))}, nil),
		Entry("from the end of an interval",
			[]timeline.Event{interval(at(3, 0, 0), at(3, 10, 0)), moment(at(3, 20, 0))},
			[]timeline.Gap{{Before: 1, Duration: 10 * time.Minute}}),
		Entry("from the end of a long interval that outlasts a shorter later one",
			[]timeline.Event{
				interval(at(3, 0, 0), at(4, 0, 0)), interval(at(3, 10, 0), at(3, 15, 0)), moment(at(4, 7, 0)),
			},
			[]timeline.Gap{{Before: 2, Duration: 7 * time.Minute}}),
		Entry("from the start of a coarse event's window",
			[]timeline.Event{
				{OccurredAt: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC), Precision: timeline.PrecisionDay},
				moment(at(0, 6, 0)),
			},
			[]timeline.Gap{{Before: 1, Duration: 6 * time.Minute}}),
		Entry("each one in order",
			[]timeline.Event{moment(at(3, 0, 0)), moment(at(3, 47, 0)), moment(at(3, 48, 0)), moment(at(5, 0, 0))},
			[]timeline.Gap{{Before: 1, Duration: 47 * time.Minute}, {Before: 3, Duration: 72 * time.Minute}}),
	)
})
