package timeparse_test

import (
	"time"
	_ "time/tzdata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

type resolved struct {
	instant     string
	offset      *int
	precision   timeline.Precision
	approximate bool
}

func at(instant string, offset *int, precision timeline.Precision) resolved {
	return resolved{instant: instant, offset: offset, precision: precision}
}

func (r resolved) approximately() resolved {
	r.approximate = true
	return r
}

func parseIn(zone, input string) (resolved, error) {
	loc, err := time.LoadLocation(zone)
	Expect(err).NotTo(HaveOccurred())

	now := time.Date(2026, 9, 12, 21, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	got, err := timeparse.Parse(input, now, loc)

	return resolved{timeline.FormatInstant(got.Instant), got.Offset, got.Precision, got.Approximate}, err
}

// The corpus pins what each input means at 2026-09-12T21:30:00-04:00. A row that changes
// after a dependency upgrade is a behaviour change to decide on, not a row to update.
var _ = Describe("Parse golden corpus", func() {
	const (
		edt = -4 * 60 * 60
		ist = 5*60*60 + 30*60
	)

	DescribeTable("resolves in America/New_York",
		func(input string, want resolved) {
			got, err := parseIn("America/New_York", input)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry(nil, "2026-09-12T14:05:03Z",
			at("2026-09-12T14:05:03.000000000Z", nil, timeline.PrecisionSecond)),
		Entry("RFC 3339 milliseconds, which olebedev/when read as 05:03 local", "2026-09-12T14:05:03.123Z",
			at("2026-09-12T14:05:03.123000000Z", nil, timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12T14:05:03.123456-04:00",
			at("2026-09-12T18:05:03.123456000Z", new(edt), timeline.PrecisionMicrosecond)),
		Entry(nil, "2026-09-12T14:05:03.123456789+05:30",
			at("2026-09-12T08:35:03.123456789Z", new(ist), timeline.PrecisionNanosecond)),
		Entry("+00:00 states a zero offset, where Z states none", "2026-09-12T14:05:03+00:00",
			at("2026-09-12T14:05:03.000000000Z", new(0), timeline.PrecisionSecond)),
		Entry("MongoDB's offset without a colon", "2026-09-12T14:05:03.000-0400",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12T14:05:03+0530",
			at("2026-09-12T08:35:03.000000000Z", new(ist), timeline.PrecisionSecond)),
		Entry("epoch seconds", "1789236000",
			at("2026-09-12T18:00:00.000000000Z", nil, timeline.PrecisionSecond)),
		Entry("epoch milliseconds", "1789236000123",
			at("2026-09-12T18:00:00.123000000Z", nil, timeline.PrecisionMillisecond)),
		Entry("epoch microseconds", "1789236000123456",
			at("2026-09-12T18:00:00.123456000Z", nil, timeline.PrecisionMicrosecond)),
		Entry("epoch nanoseconds", "1789236000123456789",
			at("2026-09-12T18:00:00.123456789Z", nil, timeline.PrecisionNanosecond)),
		Entry(nil, "~2026-09-12T14:05:03Z",
			at("2026-09-12T14:05:03.000000000Z", nil, timeline.PrecisionSecond).approximately()),
		Entry(nil, "around 2026-09-12T14:05:03-04:00",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond).approximately()),
		Entry(nil, "AROUND 1789236000",
			at("2026-09-12T18:00:00.000000000Z", nil, timeline.PrecisionSecond).approximately()),
	)

	DescribeTable("rejects",
		func(input string, kind error) {
			_, err := parseIn("America/New_York", input)
			Expect(err).To(MatchError(kind))
		},
		Entry("an empty input", "", timeparse.ErrUnrecognized),
		Entry("a bare approximate marker", "~", timeparse.ErrUnrecognized),
	)
})
