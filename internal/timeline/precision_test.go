package timeline_test

import (
	"time"
	_ "time/tzdata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Precision.Truncate", func() {
	stated := time.Date(2026, 9, 12, 10, 47, 33, 123_456_789, time.FixedZone("IST", 19800))

	DescribeTable("returns the start of the precision window in the stated offset",
		func(p timeline.Precision, want string) {
			Expect(timeline.FormatInstant(p.Truncate(stated))).To(Equal(want))
		},
		Entry("day", timeline.PrecisionDay, "2026-09-11T18:30:00.000000000Z"),
		Entry("hour, which a UTC truncation would put at 05:00", timeline.PrecisionHour, "2026-09-12T04:30:00.000000000Z"),
		Entry("minute", timeline.PrecisionMinute, "2026-09-12T05:17:00.000000000Z"),
		Entry("second", timeline.PrecisionSecond, "2026-09-12T05:17:33.000000000Z"),
		Entry("millisecond", timeline.PrecisionMillisecond, "2026-09-12T05:17:33.123000000Z"),
		Entry("microsecond", timeline.PrecisionMicrosecond, "2026-09-12T05:17:33.123456000Z"),
		Entry("nanosecond", timeline.PrecisionNanosecond, "2026-09-12T05:17:33.123456789Z"),
	)

	DescribeTable("measures a named location's window in t's own offset across a DST change",
		func(p timeline.Precision, at time.Time, want string) {
			newYork, err := time.LoadLocation("America/New_York")
			Expect(err).NotTo(HaveOccurred())

			Expect(timeline.FormatInstant(p.Truncate(at.In(newYork)))).To(Equal(want))
		},
		Entry("the repeated 01:xx hour, in EST", timeline.PrecisionHour,
			time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), "2026-11-01T06:00:00.000000000Z"),
		Entry("the day clocks spring forward, in EDT", timeline.PrecisionDay,
			time.Date(2026, 3, 8, 14, 0, 0, 0, time.UTC), "2026-03-08T04:00:00.000000000Z"),
	)
})
