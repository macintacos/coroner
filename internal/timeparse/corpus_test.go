package timeparse_test

import (
	"errors"
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
		est = -5 * 60 * 60
		pdt = -7 * 60 * 60
		ist = 5*60*60 + 30*60
	)

	DescribeTable("resolves in America/New_York",
		func(input string, want resolved) {
			got, err := parseIn("America/New_York", input)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		func(input string, _ resolved) string { return input },
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
		Entry(nil, "2026-09-12 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "2026-09-12 14:05:03.5",
			at("2026-09-12T18:05:03.500000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12 14:05:03.1234",
			at("2026-09-12T18:05:03.123400000Z", new(edt), timeline.PrecisionMicrosecond)),
		Entry("log4j and Python comma milliseconds", "2026-09-12 14:05:03,123",
			at("2026-09-12T18:05:03.123000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry("a naive time outside daylight saving", "2026-12-03 10:15:00",
			at("2026-12-03T15:15:00.000000000Z", new(est), timeline.PrecisionSecond)),
		Entry(nil, "around 2026-09-12 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond).approximately()),
		Entry("the hour after clocks spring forward", "2026-03-08 03:30:00",
			at("2026-03-08T07:30:00.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry("the repeated hour, disambiguated by an offset", "2026-11-01T01:30:00-04:00",
			at("2026-11-01T05:30:00.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry("syslog, this year", "Sep 12 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry("syslog later in the year than now, so last year", "Sep 13 14:05:03",
			at("2025-09-13T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry("syslog with a space-padded day", "Jan  2 03:04:05",
			at("2026-01-02T08:04:05.000000000Z", new(est), timeline.PrecisionSecond)),
		Entry("syslog on a leap day, the latest one before now", "Feb 29 12:00:00",
			at("2024-02-29T17:00:00.000000000Z", new(est), timeline.PrecisionSecond)),
		Entry("yesterday counted in the stated zone, where it is already Sep 13", "yesterday 14:05 UTC",
			at("2026-09-12T14:05:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "yesterday 00:30 UTC",
			at("2026-09-12T00:30:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry("the latest 00:30 in the stated zone, an hour ago", "00:30 UTC",
			at("2026-09-13T00:30:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "23:00 UTC",
			at("2026-09-12T23:00:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "14:05 UTC",
			at("2026-09-12T14:05:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "14:05 Z",
			at("2026-09-12T14:05:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "14:05 PDT",
			at("2026-09-12T21:05:00.000000000Z", new(pdt), timeline.PrecisionMinute)),
		Entry(nil, "Sep 12 2026 14:05 +0530",
			at("2026-09-12T08:35:00.000000000Z", new(ist), timeline.PrecisionMinute)),
		Entry(nil, "10 minutes ago",
			at("2026-09-13T01:20:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		Entry("an hour-precise time at the start of its hour", "3 hours ago",
			at("2026-09-12T22:00:00.000000000Z", new(edt), timeline.PrecisionHour)),
		Entry(nil, "2 seconds ago",
			at("2026-09-13T01:29:58.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "now",
			at("2026-09-13T01:30:00.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "today",
			at("2026-09-12T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "yesterday",
			at("2026-09-11T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry("an explicitly future day", "tomorrow",
			at("2026-09-13T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "yesterday 14:05",
			at("2026-09-11T18:05:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		Entry("a weekday is the latest one before now", "friday",
			at("2026-09-11T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "friday 9am",
			at("2026-09-11T13:00:00.000000000Z", new(edt), timeline.PrecisionHour)),
		Entry("a time of day later than now is last night", "23:00",
			at("2026-09-12T03:00:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		Entry("a date later in the year than now is last year", "Dec 3 10:15",
			at("2025-12-03T15:15:00.000000000Z", new(est), timeline.PrecisionMinute)),
		Entry("last week is a day, since go-dateparser has no week period", "last week",
			at("2026-09-05T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "12 September 2026 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry("a naive ISO date-time with T", "2026-09-12T14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "~yesterday 14:05",
			at("2026-09-11T18:05:00.000000000Z", new(edt), timeline.PrecisionMinute).approximately()),
		Entry(nil, "around 9am",
			at("2026-09-12T13:00:00.000000000Z", new(edt), timeline.PrecisionHour).approximately()),
	)

	DescribeTable("rejects",
		func(input string, kind error) {
			_, err := parseIn("America/New_York", input)
			Expect(err).To(MatchError(kind))
		},
		func(input string, _ error) string { return input },
		Entry("an empty input", "", timeparse.ErrUnrecognized),
		Entry("a bare approximate marker", "~", timeparse.ErrUnrecognized),
		Entry(nil, "garbage", timeparse.ErrUnrecognized),
		Entry("last with a weekday and a time, which go-dateparser cannot read", "last friday 9am",
			timeparse.ErrUnrecognized),
		Entry("a wall time clocks spring forward over", "2026-03-08 02:30:00", timeparse.ErrDSTGap),
		Entry("a wall time clocks fall back through twice", "2026-11-01 01:30:00", timeparse.ErrDSTFold),
		Entry(nil, "2026-03-08 02:30", timeparse.ErrDSTGap),
		Entry(nil, "2026-11-01 01:30", timeparse.ErrDSTFold),
		Entry("a gap time go-dateparser would move to 01:30 EST", "march 8 2026 2:30am", timeparse.ErrDSTGap),
		Entry(nil, "november 1 2026 1:30am", timeparse.ErrDSTFold),
		Entry(nil, "last month", timeparse.ErrTooCoarse),
		Entry(nil, "september 2026", timeparse.ErrTooCoarse),
		Entry("a year, which is too short for an epoch", "2025", timeparse.ErrTooCoarse),
	)

	DescribeTable("resolves in another time zone",
		func(zone, input string, want resolved) {
			got, err := parseIn(zone, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("an explicit offset wins", "Asia/Kolkata", "2026-09-12T14:05:03-04:00",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry("a naive time in UTC states a zero offset", "UTC", "2026-09-12 14:05:03",
			at("2026-09-12T14:05:03.000000000Z", new(0), timeline.PrecisionSecond)),
		Entry("a relative time takes the zone's offset", "Asia/Kolkata", "10 minutes ago",
			at("2026-09-13T01:20:00.000000000Z", new(ist), timeline.PrecisionMinute)),
	)

	It("hints at both offsets a repeated wall time could mean", func() {
		_, err := parseIn("America/New_York", "2026-11-01 01:30:00")

		rejected, ok := errors.AsType[*timeparse.ParseError](err)
		Expect(ok).To(BeTrue())
		Expect(rejected.Hint).To(And(
			ContainSubstring("2026-11-01T01:30:00-04:00"), ContainSubstring("2026-11-01T01:30:00-05:00"),
		))
	})
})
