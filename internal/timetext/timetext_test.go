package timetext_test

import (
	"time"
	_ "time/tzdata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timetext"
)

const (
	ist = 19800
	edt = -14400
	est = -18000
)

// event builds an event occurring at the RFC 3339 instant at, recorded at offset seconds east
// of UTC (UTC when nil), truncated to precision, ending at ended when it is not "".
func event(at string, offset *int, precision timeline.Precision, approximate bool, ended string) timeline.Event {
	occurred, err := time.Parse(time.RFC3339Nano, at)
	Expect(err).NotTo(HaveOccurred())
	own := 0
	if offset != nil {
		own = *offset
	}
	e := timeline.Event{
		OccurredAt:     precision.Truncate(occurred.In(time.FixedZone("", own))).UTC(),
		OccurredOffset: offset,
		Precision:      precision,
		Approximate:    approximate,
	}
	if ended != "" {
		end, err := time.Parse(time.RFC3339Nano, ended)
		Expect(err).NotTo(HaveOccurred())
		e.EndedAt = &end
	}

	return e
}

// fixture returns the events E1–E16 by name.
func fixture() map[string]timeline.Event {
	const fraction = "2026-09-11T03:14:07.180342917Z"

	return map[string]timeline.Event{
		"E1":  event("2026-09-11T05:00:00Z", new(est), timeline.PrecisionDay, false, ""),
		"E2":  event("2026-09-11T00:00:00Z", new(0), timeline.PrecisionDay, false, "2026-09-12T00:00:00Z"),
		"E3":  event("2026-09-11T04:30:00Z", new(ist), timeline.PrecisionHour, false, ""),
		"E4":  event("2026-09-11T03:00:00Z", new(0), timeline.PrecisionHour, false, ""),
		"E5":  event("2026-09-11T03:14:00Z", new(0), timeline.PrecisionMinute, true, ""),
		"E6":  event(fraction, new(0), timeline.PrecisionSecond, false, ""),
		"E7":  event(fraction, new(0), timeline.PrecisionMillisecond, false, ""),
		"E8":  event(fraction, new(0), timeline.PrecisionMicrosecond, false, ""),
		"E9":  event(fraction, new(0), timeline.PrecisionNanosecond, false, ""),
		"E10": event("2026-09-11T03:15:00Z", new(0), timeline.PrecisionSecond, false, "2026-09-11T03:16:30Z"),
		"E11": event("2026-09-11T23:50:00Z", new(0), timeline.PrecisionMinute, false, "2026-09-12T00:20:00Z"),
		"E12": event("2026-09-12T00:00:00Z", new(0), timeline.PrecisionDay, true, ""),
		"E13": event("2026-09-11T20:30:00Z", new(ist), timeline.PrecisionHour, false, ""),
		"E14": event("2026-09-11T20:30:00Z", new(ist), timeline.PrecisionHour, true, "2026-09-11T22:30:00Z"),
		"E15": event("2026-11-01T05:30:00Z", new(edt), timeline.PrecisionSecond, false, ""),
		"E16": event("2026-11-01T06:30:00Z", new(est), timeline.PrecisionSecond, false, "2026-11-01T06:45:00Z"),
	}
}

func inUTC() *time.Location { return time.UTC }

func newYork() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	Expect(err).NotTo(HaveOccurred())

	return loc
}

func part(date, clock, abbreviation, offset string) timetext.Part {
	return timetext.Part{Date: date, Clock: clock, Abbreviation: abbreviation, Offset: offset}
}

func utc(date, clock string) timetext.Part { return part(date, clock, "UTC", "+00:00") }

// own is a part read in the event's own offset, which has no abbreviation.
func own(date, clock, offset string) timetext.Part {
	return timetext.Part{Date: date, Clock: clock, Offset: offset, OwnOffset: true}
}

func edtPart(date, clock string) timetext.Part { return part(date, clock, "EDT", "-04:00") }

func reading(day string, start timetext.Part, end *timetext.Part, approximate bool, precision timeline.Precision) timetext.Reading {
	return timetext.Reading{Day: day, Start: start, End: end, Approximate: approximate, Precision: precision}
}

func readAll(zone *time.Location, names ...string) []timetext.Reading {
	events := fixture()
	readings := make([]timetext.Reading, 0, len(names))
	for _, name := range names {
		readings = append(readings, timetext.Read(events[name], zone))
	}

	return readings
}

var _ = Describe("timetext", func() {
	DescribeTable("Read shows an event's time at its precision",
		func(precision timeline.Precision, want string) {
			e := event("2026-09-11T03:14:07.180342917Z", nil, precision, false, "")
			Expect(timetext.Read(e, time.UTC).Start.Clock).To(Equal(want))
		},
		Entry("day", timeline.PrecisionDay, ""),
		Entry("hour", timeline.PrecisionHour, "03:--"),
		Entry("minute", timeline.PrecisionMinute, "03:14"),
		Entry("second", timeline.PrecisionSecond, "03:14:07"),
		Entry("millisecond", timeline.PrecisionMillisecond, "03:14:07.180"),
		Entry("microsecond", timeline.PrecisionMicrosecond, "03:14:07.180342"),
		Entry("nanosecond", timeline.PrecisionNanosecond, "03:14:07.180342917"),
	)

	It("marks approximate times", func() {
		Expect(readAll(time.UTC, "E5", "E6")).To(HaveExactElements(
			HaveField("Approximate", true),
			HaveField("Approximate", false),
		))
	})

	DescribeTable("DayZone joins abbreviations in order and reports a change of offset",
		func(zone func() *time.Location, names []string, wantLabel string, wantSpans bool) {
			label, spans := timetext.DayZone(readAll(zone(), names...), "fallback")
			Expect(label).To(Equal(wantLabel))
			Expect(spans).To(Equal(wantSpans))
		},
		Entry("an ordinary day", newYork, []string{"E11", "E13", "E14"}, "EDT", false),
		Entry("the fall-back day", newYork, []string{"E15", "E16"}, "EDT/EST", true),
		Entry("a day of day-precision events only", newYork, []string{"E1", "E2"}, "fallback", false),
		Entry("UTC", inUTC, []string{"E3", "E5", "E10"}, "UTC", false),
	)

	DescribeTable("Read reads a coarse window that does not line up in the zone in its own offset",
		func(e timeline.Event, zone func() *time.Location, want timetext.Part) {
			Expect(timetext.Read(e, zone()).Start).To(Equal(want))
		},
		Entry("an hour at +05:30 in UTC", event("2026-09-11T04:30:00Z", new(ist), timeline.PrecisionHour, false, ""), inUTC, own("2026-09-11", "10:--", "+05:30")),
		Entry("an hour at +05:30 in New York", event("2026-09-11T04:30:00Z", new(ist), timeline.PrecisionHour, false, ""), newYork, own("2026-09-11", "10:--", "+05:30")),
		Entry("a day at -05:00 in UTC", event("2026-09-11T05:00:00Z", new(est), timeline.PrecisionDay, false, ""), inUTC, own("2026-09-11", "", "-05:00")),
		Entry("an hour with no offset, read as UTC", event("2026-09-11T03:00:00Z", nil, timeline.PrecisionHour, false, ""), newYork, edtPart("2026-09-10", "23:--")),
		Entry("a day with no offset, read as UTC", event("2026-09-11T00:00:00Z", nil, timeline.PrecisionDay, false, ""), newYork, timetext.Part{Date: "2026-09-11", Abbreviation: "UTC", Offset: "+00:00", OwnOffset: true}),
		Entry("a window that lines up", event("2026-09-11T03:00:00Z", new(0), timeline.PrecisionHour, false, ""), newYork, edtPart("2026-09-10", "23:--")),
	)

	DescribeTable("Read files an event under its instant's day in the zone and dates a part on another day",
		func(e timeline.Event, zone func() *time.Location, wantDay, wantStart, wantEnd string) {
			r := timetext.Read(e, zone())
			Expect(r.Day).To(Equal(wantDay))
			Expect(r.Start.Date).To(Equal(wantStart))
			if wantEnd == "" {
				Expect(r.End).To(BeNil())
			} else {
				Expect(r.End).To(HaveField("Date", wantEnd))
			}
		},
		Entry("E13 in UTC", fixture()["E13"], inUTC, "2026-09-11", "2026-09-12", ""),
		Entry("a cross-midnight end", fixture()["E11"], inUTC, "2026-09-11", "2026-09-11", "2026-09-12"),
		Entry("a cross-day end", fixture()["E14"], inUTC, "2026-09-11", "2026-09-12", "2026-09-12"),
		Entry("E2's end in its own offset in New York", fixture()["E2"], newYork, "2026-09-10", "2026-09-11", "2026-09-12"),
	)

	DescribeTable("Location is the zone when the event lines up in it, else the event's own offset",
		func(e timeline.Event, zone func() *time.Location, wantOffset int, wantZone bool) {
			loc := timetext.Location(e, zone())
			if wantZone {
				Expect(loc).To(Equal(zone()))
			}
			_, offset := e.OccurredAt.In(loc).Zone()
			Expect(offset).To(Equal(wantOffset))
		},
		Entry("an hour that lines up", fixture()["E4"], newYork, edt, true),
		Entry("an hour at +05:30 in New York", fixture()["E3"], newYork, ist, false),
		Entry("a day at -05:00 in UTC", fixture()["E1"], inUTC, est, false),
		Entry("a day with no offset", event("2026-09-11T00:00:00Z", nil, timeline.PrecisionDay, false, ""), newYork, 0, false),
	)

	It("Stamp writes the minute and the zone's abbreviation then", func() {
		at := time.Date(2026, 11, 1, 6, 30, 59, 0, time.UTC)
		Expect(timetext.Stamp(at, newYork())).To(Equal("2026-11-01 01:30 EST"))
	})

	DescribeTable("Read reads the fixture",
		func(name string, zone func() *time.Location, want timetext.Reading) {
			Expect(timetext.Read(fixture()[name], zone())).To(Equal(want))
		},
		func(name string, zone func() *time.Location, _ timetext.Reading) string {
			return name + " in " + zone().String()
		},
		Entry(nil, "E1", inUTC, reading("2026-09-11", own("2026-09-11", "", "-05:00"), nil, false, timeline.PrecisionDay)),
		Entry(nil, "E2", inUTC, reading("2026-09-11", utc("2026-09-11", ""), new(utc("2026-09-12", "")), false, timeline.PrecisionDay)),
		Entry(nil, "E3", inUTC, reading("2026-09-11", own("2026-09-11", "10:--", "+05:30"), nil, false, timeline.PrecisionHour)),
		Entry(nil, "E4", inUTC, reading("2026-09-11", utc("2026-09-11", "03:--"), nil, false, timeline.PrecisionHour)),
		Entry(nil, "E5", inUTC, reading("2026-09-11", utc("2026-09-11", "03:14"), nil, true, timeline.PrecisionMinute)),
		Entry(nil, "E6", inUTC, reading("2026-09-11", utc("2026-09-11", "03:14:07"), nil, false, timeline.PrecisionSecond)),
		Entry(nil, "E7", inUTC, reading("2026-09-11", utc("2026-09-11", "03:14:07.180"), nil, false, timeline.PrecisionMillisecond)),
		Entry(nil, "E8", inUTC, reading("2026-09-11", utc("2026-09-11", "03:14:07.180342"), nil, false, timeline.PrecisionMicrosecond)),
		Entry(nil, "E9", inUTC, reading("2026-09-11", utc("2026-09-11", "03:14:07.180342917"), nil, false, timeline.PrecisionNanosecond)),
		Entry(nil, "E10", inUTC, reading("2026-09-11", utc("2026-09-11", "03:15:00"), new(utc("2026-09-11", "03:16:30")), false, timeline.PrecisionSecond)),
		Entry(nil, "E11", inUTC, reading("2026-09-11", utc("2026-09-11", "23:50"), new(utc("2026-09-12", "00:20")), false, timeline.PrecisionMinute)),
		Entry(nil, "E12", inUTC, reading("2026-09-12", utc("2026-09-12", ""), nil, true, timeline.PrecisionDay)),
		Entry(nil, "E13", inUTC, reading("2026-09-11", own("2026-09-12", "02:--", "+05:30"), nil, false, timeline.PrecisionHour)),
		Entry(nil, "E14", inUTC, reading("2026-09-11", own("2026-09-12", "02:--", "+05:30"), new(own("2026-09-12", "04:--", "+05:30")), true, timeline.PrecisionHour)),
		Entry(nil, "E15", inUTC, reading("2026-11-01", utc("2026-11-01", "05:30:00"), nil, false, timeline.PrecisionSecond)),
		Entry(nil, "E16", inUTC, reading("2026-11-01", utc("2026-11-01", "06:30:00"), new(utc("2026-11-01", "06:45:00")), false, timeline.PrecisionSecond)),

		Entry(nil, "E1", newYork, reading("2026-09-11", own("2026-09-11", "", "-05:00"), nil, false, timeline.PrecisionDay)),
		Entry(nil, "E2", newYork, reading("2026-09-10", own("2026-09-11", "", "+00:00"), new(own("2026-09-12", "", "+00:00")), false, timeline.PrecisionDay)),
		Entry(nil, "E3", newYork, reading("2026-09-11", own("2026-09-11", "10:--", "+05:30"), nil, false, timeline.PrecisionHour)),
		Entry(nil, "E4", newYork, reading("2026-09-10", edtPart("2026-09-10", "23:--"), nil, false, timeline.PrecisionHour)),
		Entry(nil, "E5", newYork, reading("2026-09-10", edtPart("2026-09-10", "23:14"), nil, true, timeline.PrecisionMinute)),
		Entry(nil, "E6", newYork, reading("2026-09-10", edtPart("2026-09-10", "23:14:07"), nil, false, timeline.PrecisionSecond)),
		Entry(nil, "E7", newYork, reading("2026-09-10", edtPart("2026-09-10", "23:14:07.180"), nil, false, timeline.PrecisionMillisecond)),
		Entry(nil, "E8", newYork, reading("2026-09-10", edtPart("2026-09-10", "23:14:07.180342"), nil, false, timeline.PrecisionMicrosecond)),
		Entry(nil, "E9", newYork, reading("2026-09-10", edtPart("2026-09-10", "23:14:07.180342917"), nil, false, timeline.PrecisionNanosecond)),
		Entry(nil, "E10", newYork, reading("2026-09-10", edtPart("2026-09-10", "23:15:00"), new(edtPart("2026-09-10", "23:16:30")), false, timeline.PrecisionSecond)),
		Entry(nil, "E11", newYork, reading("2026-09-11", edtPart("2026-09-11", "19:50"), new(edtPart("2026-09-11", "20:20")), false, timeline.PrecisionMinute)),
		Entry(nil, "E12", newYork, reading("2026-09-11", own("2026-09-12", "", "+00:00"), nil, true, timeline.PrecisionDay)),
		Entry(nil, "E13", newYork, reading("2026-09-11", own("2026-09-12", "02:--", "+05:30"), nil, false, timeline.PrecisionHour)),
		Entry(nil, "E14", newYork, reading("2026-09-11", own("2026-09-12", "02:--", "+05:30"), new(own("2026-09-12", "04:--", "+05:30")), true, timeline.PrecisionHour)),
		Entry(nil, "E15", newYork, reading("2026-11-01", edtPart("2026-11-01", "01:30:00"), nil, false, timeline.PrecisionSecond)),
		Entry(nil, "E16", newYork, reading("2026-11-01", part("2026-11-01", "01:30:00", "EST", "-05:00"), new(part("2026-11-01", "01:45:00", "EST", "-05:00")), false, timeline.PrecisionSecond)),
	)
})
