package timeparse_test

import (
	"errors"
	"time"
	_ "time/tzdata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

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
	return parseAt(time.Date(2026, 9, 12, 21, 30, 0, 0, time.FixedZone("EDT", -4*60*60)), zone, input)
}

func parseAt(now time.Time, zone, input string) (resolved, error) {
	loc, err := time.LoadLocation(zone)
	Expect(err).NotTo(HaveOccurred())

	got, err := timeparse.Parse(input, now, loc)

	return resolved{timeline.FormatInstant(got.Instant), got.Offset, got.Precision, got.Approximate}, err
}

func utc(instant string) time.Time {
	t, err := time.Parse(time.RFC3339, instant)
	Expect(err).NotTo(HaveOccurred())

	return t
}

// The corpus pins what each input means at 2026-09-12T21:30:00-04:00 unless a row states
// its own now. A row that changes after a dependency upgrade is a behaviour change to
// decide on, not a row to update.
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
		Entry(nil, "2026-09-12T14:05:03.123Z",
			at("2026-09-12T14:05:03.123000000Z", nil, timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12T14:05:03.123456-04:00",
			at("2026-09-12T18:05:03.123456000Z", new(edt), timeline.PrecisionMicrosecond)),
		Entry(nil, "2026-09-12T14:05:03.123456789+05:30",
			at("2026-09-12T08:35:03.123456789Z", new(ist), timeline.PrecisionNanosecond)),
		// +00:00 states a zero offset, where Z states none.
		Entry(nil, "2026-09-12T14:05:03+00:00",
			at("2026-09-12T14:05:03.000000000Z", new(0), timeline.PrecisionSecond)),
		// -00:00 states UTC with the local offset unknown, as Z does.
		Entry(nil, "2026-09-12T14:05:03-00:00",
			at("2026-09-12T14:05:03.000000000Z", nil, timeline.PrecisionSecond)),
		Entry(nil, "2026-09-12T14:05:03-0000",
			at("2026-09-12T14:05:03.000000000Z", nil, timeline.PrecisionSecond)),
		// MongoDB's offset without a colon.
		Entry(nil, "2026-09-12T14:05:03.000-0400",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12T14:05:03+0530",
			at("2026-09-12T08:35:03.000000000Z", new(ist), timeline.PrecisionSecond)),
		Entry(nil, "2026-09-12 14:05:03.123Z",
			at("2026-09-12T14:05:03.123000000Z", nil, timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12 14:05:03.123456+05:30",
			at("2026-09-12T08:35:03.123456000Z", new(ist), timeline.PrecisionMicrosecond)),
		// RFC 3339 allows a lowercase t and z.
		Entry(nil, "2026-09-12t14:05:03.123z",
			at("2026-09-12T14:05:03.123000000Z", nil, timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12T14:05Z",
			at("2026-09-12T14:05:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "1789236000",
			at("2026-09-12T18:00:00.000000000Z", nil, timeline.PrecisionSecond)),
		Entry(nil, "1789236000123",
			at("2026-09-12T18:00:00.123000000Z", nil, timeline.PrecisionMillisecond)),
		Entry(nil, "1789236000123456",
			at("2026-09-12T18:00:00.123456000Z", nil, timeline.PrecisionMicrosecond)),
		Entry(nil, "1789236000123456789",
			at("2026-09-12T18:00:00.123456789Z", nil, timeline.PrecisionNanosecond)),
		Entry(nil, "1789236000.5",
			at("2026-09-12T18:00:00.500000000Z", nil, timeline.PrecisionMillisecond)),
		// GNU date's epoch marker.
		Entry(nil, "@1789236000",
			at("2026-09-12T18:00:00.000000000Z", nil, timeline.PrecisionSecond)),
		Entry(nil, "~2026-09-12T14:05:03Z",
			at("2026-09-12T14:05:03.000000000Z", nil, timeline.PrecisionSecond).approximately()),
		Entry(nil, "around 2026-09-12T14:05:03-04:00",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond).approximately()),
		Entry(nil, "AROUND 1789236000",
			at("2026-09-12T18:00:00.000000000Z", nil, timeline.PrecisionSecond).approximately()),
		Entry(nil, "2026-09-12 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "2026-09-12T14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "2026-09-12T14:05:03.123",
			at("2026-09-12T18:05:03.123000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12 14:05:03.5",
			at("2026-09-12T18:05:03.500000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12 14:05:03.1234",
			at("2026-09-12T18:05:03.123400000Z", new(edt), timeline.PrecisionMicrosecond)),
		// log4j and Python comma milliseconds.
		Entry(nil, "2026-09-12 14:05:03,123",
			at("2026-09-12T18:05:03.123000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry(nil, "2026-09-12 14:05",
			at("2026-09-12T18:05:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		// A naive time outside daylight saving.
		Entry(nil, "2026-12-03 10:15:00",
			at("2026-12-03T15:15:00.000000000Z", new(est), timeline.PrecisionSecond)),
		Entry(nil, "around 2026-09-12 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond).approximately()),
		// The hour after clocks spring forward.
		Entry(nil, "2026-03-08 03:30:00",
			at("2026-03-08T07:30:00.000000000Z", new(edt), timeline.PrecisionSecond)),
		// The repeated hour, disambiguated by an offset.
		Entry(nil, "2026-11-01T01:30:00-04:00",
			at("2026-11-01T05:30:00.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "Sep 12 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		// Syslog later in the year than now, so last year.
		Entry(nil, "Sep 13 14:05:03",
			at("2025-09-13T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		// Syslog with a space-padded day.
		Entry(nil, "Jan  2 03:04:05",
			at("2026-01-02T08:04:05.000000000Z", new(est), timeline.PrecisionSecond)),
		// Syslog on a leap day, the latest one before now.
		Entry(nil, "Feb 29 12:00:00",
			at("2024-02-29T17:00:00.000000000Z", new(est), timeline.PrecisionSecond)),
		// Yesterday counted in the stated zone, where it is already Sep 13.
		Entry(nil, "yesterday 14:05 UTC",
			at("2026-09-12T14:05:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "yesterday 00:30 UTC",
			at("2026-09-12T00:30:00.000000000Z", nil, timeline.PrecisionMinute)),
		// The latest 00:30 in the stated zone, an hour ago.
		Entry(nil, "00:30 UTC",
			at("2026-09-13T00:30:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "23:00 UTC",
			at("2026-09-12T23:00:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "14:05 UTC",
			at("2026-09-12T14:05:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "14:05 Z",
			at("2026-09-12T14:05:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "14:05 PDT",
			at("2026-09-12T21:05:00.000000000Z", new(pdt), timeline.PrecisionMinute)),
		// Half an hour ago in PDT.
		Entry(nil, "18:00 PDT",
			at("2026-09-13T01:00:00.000000000Z", new(pdt), timeline.PrecisionMinute)),
		// A time of day later than now in the stated zone is the previous evening there.
		Entry(nil, "20:00 PDT",
			at("2026-09-12T03:00:00.000000000Z", new(pdt), timeline.PrecisionMinute)),
		Entry(nil, "22:00 -0400",
			at("2026-09-12T02:00:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		Entry(nil, "23:30 -0700",
			at("2026-09-12T06:30:00.000000000Z", new(pdt), timeline.PrecisionMinute)),
		Entry(nil, "09:00 +0530",
			at("2026-09-12T03:30:00.000000000Z", new(ist), timeline.PrecisionMinute)),
		Entry(nil, "Sep 12 2026 14:05 +0530",
			at("2026-09-12T08:35:00.000000000Z", new(ist), timeline.PrecisionMinute)),
		Entry(nil, "10 minutes ago",
			at("2026-09-13T01:20:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		// An hour-precise time at the start of its hour.
		Entry(nil, "3 hours ago",
			at("2026-09-12T22:00:00.000000000Z", new(edt), timeline.PrecisionHour)),
		Entry(nil, "2 seconds ago",
			at("2026-09-13T01:29:58.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "2 weeks ago",
			at("2026-08-29T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "now",
			at("2026-09-13T01:30:00.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "today",
			at("2026-09-12T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "yesterday",
			at("2026-09-11T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "yesterday 14:05",
			at("2026-09-11T18:05:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		// A weekday is the latest one before now.
		Entry(nil, "friday",
			at("2026-09-11T04:00:00.000000000Z", new(edt), timeline.PrecisionDay)),
		Entry(nil, "friday 9am",
			at("2026-09-11T13:00:00.000000000Z", new(edt), timeline.PrecisionHour)),
		// A time of day later than now is last night.
		Entry(nil, "23:00",
			at("2026-09-12T03:00:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		// A date later in the year than now is last year.
		Entry(nil, "Dec 3 10:15",
			at("2025-12-03T15:15:00.000000000Z", new(est), timeline.PrecisionMinute)),
		Entry(nil, "12 September 2026 14:05:03",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		// A zone name is not an offset, so natural language reads it.
		Entry(nil, "2026-09-12 14:05:03 EDT",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		Entry(nil, "14:05:03.250",
			at("2026-09-12T18:05:03.250000000Z", new(edt), timeline.PrecisionMillisecond)),
		Entry(nil, "Sep 12 14:05:03.123 UTC",
			at("2026-09-12T14:05:03.123000000Z", nil, timeline.PrecisionMillisecond)),
		Entry(nil, "~yesterday 14:05",
			at("2026-09-11T18:05:00.000000000Z", new(edt), timeline.PrecisionMinute).approximately()),
		Entry(nil, "around 9am",
			at("2026-09-12T13:00:00.000000000Z", new(edt), timeline.PrecisionHour).approximately()),
		Entry(nil, "around\t9am",
			at("2026-09-12T13:00:00.000000000Z", new(edt), timeline.PrecisionHour).approximately()),
	)

	DescribeTable("rejects",
		func(input string, kind error) {
			_, err := parseIn("America/New_York", input)
			Expect(err).To(MatchError(kind))
		},
		func(input string, _ error) string { return input },
		Entry(nil, "", timeparse.ErrUnrecognized),
		Entry(nil, "~", timeparse.ErrUnrecognized),
		Entry(nil, "garbage", timeparse.ErrUnrecognized),
		// go-dateparser cannot read last with a weekday and a time.
		Entry(nil, "last friday 9am", timeparse.ErrUnrecognized),
		// Too many digits for epoch seconds, too few for milliseconds.
		Entry(nil, "12345678901", timeparse.ErrUnrecognized),
		Entry(nil, "20260912", timeparse.ErrUnrecognized),
		// Epoch nanoseconds past 2262-04-11 overflow 64 bits.
		Entry(nil, "9999999999999999999", timeparse.ErrUnrecognized),
		// Malformed input in an exact format is rejected, never repaired by natural language.
		Entry(nil, "2026-13-01T00:00:00Z", timeparse.ErrUnrecognized),
		Entry(nil, "2026-02-30T10:00:00Z", timeparse.ErrUnrecognized),
		Entry(nil, "2026-09-12T14:05:03+5:30", timeparse.ErrUnrecognized),
		Entry(nil, "2026-09-31 10:00", timeparse.ErrUnrecognized),
		Entry(nil, "Feb 30 10:00:00", timeparse.ErrUnrecognized),
		Entry(nil, "1789236000123.456", timeparse.ErrUnrecognized),
		Entry(nil, "1789236000123456.5", timeparse.ErrUnrecognized),
		Entry(nil, "1789236000.1234567890", timeparse.ErrUnrecognized),
		Entry(nil, "1789236000.", timeparse.ErrUnrecognized),
		Entry(nil, "2026-03-08 02:30:00", timeparse.ErrDSTGap),
		Entry(nil, "2026-11-01 01:30:00", timeparse.ErrDSTFold),
		Entry(nil, "2026-03-08 02:30", timeparse.ErrDSTGap),
		Entry(nil, "2026-11-01 01:30", timeparse.ErrDSTFold),
		// go-dateparser alone would move this gap time to 01:30 EST.
		Entry(nil, "march 8 2026 2:30am", timeparse.ErrDSTGap),
		Entry(nil, "november 2 2025 1:30am", timeparse.ErrDSTFold),
		// go-dateparser has no week period, so it would read these as a day.
		Entry(nil, "last week", timeparse.ErrTooCoarse),
		Entry(nil, "this week", timeparse.ErrTooCoarse),
		Entry(nil, "last month", timeparse.ErrTooCoarse),
		Entry(nil, "september 2026", timeparse.ErrTooCoarse),
		// A year, too short for an epoch.
		Entry(nil, "2025", timeparse.ErrTooCoarse),
		Entry(nil, "tomorrow", timeparse.ErrFuture),
		Entry(nil, "in 2 hours", timeparse.ErrFuture),
	)

	DescribeTable("resolves in another time zone",
		func(zone, input string, want resolved) {
			got, err := parseIn(zone, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		func(zone, input string, _ resolved) string { return input + " in " + zone },
		// An explicit offset wins.
		Entry(nil, "Asia/Kolkata", "2026-09-12T14:05:03-04:00",
			at("2026-09-12T18:05:03.000000000Z", new(edt), timeline.PrecisionSecond)),
		// A time read in UTC says nothing about the local offset, as Z does.
		Entry(nil, "UTC", "2026-09-12 14:05:03",
			at("2026-09-12T14:05:03.000000000Z", nil, timeline.PrecisionSecond)),
		Entry(nil, "UTC", "10 minutes ago",
			at("2026-09-13T01:20:00.000000000Z", nil, timeline.PrecisionMinute)),
		Entry(nil, "UTC", "2026-09-06",
			at("2026-09-06T00:00:00.000000000Z", nil, timeline.PrecisionDay)),
		// A named zone at +00:00 states its offset.
		Entry(nil, "Europe/London", "2026-01-12 14:05:03",
			at("2026-01-12T14:05:03.000000000Z", new(0), timeline.PrecisionSecond)),
		// A relative time takes the zone's offset.
		Entry(nil, "Asia/Kolkata", "10 minutes ago",
			at("2026-09-13T01:20:00.000000000Z", new(ist), timeline.PrecisionMinute)),
		// Santiago's clocks spring forward at midnight, so the day starts at the 00:00 before
		// the change.
		Entry(nil, "America/Santiago", "2026-09-06",
			at("2026-09-06T04:00:00.000000000Z", new(-4*60*60), timeline.PrecisionDay)),
	)

	DescribeTable("resolves at its own now",
		func(now time.Time, zone, input string, want resolved) {
			got, err := parseAt(now, zone, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		func(now time.Time, zone, input string, _ resolved) string {
			return input + " in " + zone + " at " + now.Format(time.RFC3339)
		},
		// Now is 01:30 in +05:30.
		Entry(nil, utc("2026-09-12T20:00:00Z"), "America/New_York", "01:00 +0530",
			at("2026-09-12T19:30:00.000000000Z", new(ist), timeline.PrecisionMinute)),
		Entry(nil, utc("2026-09-12T20:00:00Z"), "America/New_York", "yesterday 01:00 +0530",
			at("2026-09-11T19:30:00.000000000Z", new(ist), timeline.PrecisionMinute)),
		// Now is 01:45 EST, in the repeated hour; 70 minutes earlier was 01:35 EDT.
		Entry(nil, utc("2026-11-01T06:45:00Z"), "America/New_York", "70 minutes ago",
			at("2026-11-01T05:35:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		Entry(nil, utc("2026-11-01T06:45:00Z"), "America/New_York", "now",
			at("2026-11-01T06:45:00.000000000Z", new(est), timeline.PrecisionSecond)),
		Entry(nil, utc("2026-11-01T06:45:00Z"), "America/New_York", "23:00",
			at("2026-11-01T03:00:00.000000000Z", new(edt), timeline.PrecisionMinute)),
		// Now is 04:30 EDT; two hours earlier was 01:30 EST, before the skipped hour.
		Entry(nil, utc("2026-03-08T08:30:00Z"), "America/New_York", "2 hours ago",
			at("2026-03-08T06:00:00.000000000Z", new(est), timeline.PrecisionHour)),
	)

	DescribeTable("hints at a fix",
		func(input string, hint types.GomegaMatcher) {
			_, err := parseIn("America/New_York", input)

			rejected, ok := errors.AsType[*timeparse.ParseError](err)
			Expect(ok).To(BeTrue())
			Expect(rejected.Hint).To(hint)
		},
		func(input string, _ types.GomegaMatcher) string { return input },
		Entry(nil, "2026-11-01 01:30:00", And(
			ContainSubstring("2026-11-01T01:30:00-04:00"), ContainSubstring("2026-11-01T01:30:00-05:00"),
		)),
		Entry(nil, "2026-03-08 02:30:00", And(
			ContainSubstring("2026-03-08T02:30:00-05:00"), ContainSubstring("2026-03-08T02:30:00-04:00"),
		)),
		Entry(nil, "12345678901", ContainSubstring("10, 13, 16 or 19 digits")),
	)
})
