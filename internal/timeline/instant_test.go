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
	DescribeTable("reads the canonical form back as the same UTC instant, which formats back to it",
		func(s string, want time.Time) {
			t, err := timeline.ParseInstant(s)

			Expect(err).NotTo(HaveOccurred())
			Expect(t).To(Equal(want))
			Expect(timeline.FormatInstant(t)).To(Equal(s))
		},
		Entry("a nanosecond fraction", "2026-09-12T14:05:03.123456789Z", time.Date(2026, 9, 12, 14, 5, 3, 123_456_789, time.UTC)),
		Entry("the first instant of year 0000", "0000-01-01T00:00:00.000000000Z", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)),
		Entry("the last instant of year 9999", "9999-12-31T23:59:59.999999999Z", time.Date(9999, 12, 31, 23, 59, 59, 999_999_999, time.UTC)),
	)

	DescribeTable("rejects every non-canonical spelling",
		func(s string) {
			_, err := timeline.ParseInstant(s)
			Expect(err).To(HaveOccurred())
		},
		Entry("no fraction", "2026-09-12T14:05:03Z"),
		Entry("trimmed fraction", "2026-09-12T14:05:03.1Z"),
		Entry("numeric offset", "2026-09-12T14:05:03.000000000+00:00"),
		Entry("one-digit hour", "2026-09-12T4:05:03.000000000Z"),
		Entry("comma before the fraction", "2026-09-12T14:05:03,000000000Z"),
		Entry("space separator", "2026-09-12 14:05:03.000000000Z"),
	)
})
