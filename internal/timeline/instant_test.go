package timeline_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("FormatInstant", func() {
	DescribeTable("renders the instant as 30-character UTC text whatever the location",
		func(t time.Time) {
			Expect(timeline.FormatInstant(t)).To(Equal("2026-09-12T14:05:03.180000000Z"))
		},
		Entry("UTC", time.Date(2026, 9, 12, 14, 5, 3, 180_000_000, time.UTC)),
		Entry("+05:30", time.Date(2026, 9, 12, 19, 35, 3, 180_000_000, time.FixedZone("IST", 19800))),
		Entry("-07:00", time.Date(2026, 9, 12, 7, 5, 3, 180_000_000, time.FixedZone("PDT", -25200))),
	)

	It("keeps a whole second at full width so it sorts before a later fraction", func() {
		whole := timeline.FormatInstant(time.Date(2026, 9, 12, 14, 5, 3, 0, time.UTC))
		later := timeline.FormatInstant(time.Date(2026, 9, 12, 14, 5, 3, 100_000_000, time.UTC))

		Expect(whole).To(Equal("2026-09-12T14:05:03.000000000Z"))
		Expect(whole < later).To(BeTrue())
	})
})

var _ = Describe("ParseInstant", func() {
	It("reads the canonical form back as the same UTC instant", func() {
		t, err := timeline.ParseInstant("2026-09-12T14:05:03.123456789Z")

		Expect(err).NotTo(HaveOccurred())
		Expect(t).To(Equal(time.Date(2026, 9, 12, 14, 5, 3, 123_456_789, time.UTC)))
	})

	DescribeTable("rejects every non-canonical spelling",
		func(s string) {
			_, err := timeline.ParseInstant(s)
			Expect(err).To(HaveOccurred())
		},
		Entry("no fraction", "2026-09-12T14:05:03Z"),
		Entry("trimmed fraction", "2026-09-12T14:05:03.1Z"),
		Entry("numeric offset", "2026-09-12T14:05:03.000000000+00:00"),
		Entry("one-digit hour", "2026-09-12T4:05:03.000000000Z"),
		Entry("space separator", "2026-09-12 14:05:03.000000000Z"),
	)
})

var _ = Describe("Precision.Truncate", func() {
	observed := time.Date(2026, 9, 12, 10, 47, 33, 123_456_789, time.FixedZone("IST", 19800))

	DescribeTable("returns the start of the precision window in the observed zone",
		func(p timeline.Precision, want string) {
			Expect(timeline.FormatInstant(p.Truncate(observed))).To(Equal(want))
		},
		Entry("day", timeline.PrecisionDay, "2026-09-11T18:30:00.000000000Z"),
		Entry("hour, which a UTC truncation would put at 05:00", timeline.PrecisionHour, "2026-09-12T04:30:00.000000000Z"),
		Entry("minute", timeline.PrecisionMinute, "2026-09-12T05:17:00.000000000Z"),
		Entry("second", timeline.PrecisionSecond, "2026-09-12T05:17:33.000000000Z"),
		Entry("millisecond", timeline.PrecisionMillisecond, "2026-09-12T05:17:33.123000000Z"),
		Entry("microsecond", timeline.PrecisionMicrosecond, "2026-09-12T05:17:33.123456000Z"),
		Entry("nanosecond", timeline.PrecisionNanosecond, "2026-09-12T05:17:33.123456789Z"),
	)
})
