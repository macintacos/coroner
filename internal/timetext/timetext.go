// Package timetext is how an event's time reads to a person: the clock to its precision, the
// day it falls on, its interval's end and the zone it is shown in.
package timetext

import (
	"slices"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

// Reading is one event's time as people see it in a zone.
type Reading struct {
	// Day is the date, 2006-01-02, that a list of events in the zone files the event under.
	Day         string
	Start       Part
	End         *Part // nil for an event at a moment
	Approximate bool
	Precision   timeline.Precision
}

// Part is one end of an event's time.
type Part struct {
	// Date is the part's own date, 2006-01-02; a renderer shows it when it differs from Day.
	Date string
	// Clock is the wall clock to the precision, such as 10:-- or 03:14:07.180; "" for a day.
	Clock string
	// Abbreviation and Offset ("+05:30") are the zone's, or the event's own offset's when
	// OwnOffset is set, which a renderer must then show.
	Abbreviation, Offset string
	OwnOffset            bool
}

// Read returns e's time as it reads in zone, filed under the day its instant falls on there. A
// window that does not line up in zone reads, end included, in e's own offset; an unknown
// precision reads as the second.
func Read(e timeline.Event, zone *time.Location) Reading {
	loc := Location(e, zone)
	own := loc != zone
	start := part(e.OccurredAt.In(loc), e.Precision, own)
	r := Reading{
		Day:         e.OccurredAt.In(zone).Format(time.DateOnly),
		Start:       start,
		Approximate: e.Approximate,
		Precision:   e.Precision,
	}
	if e.EndedAt != nil {
		end := part(e.EndedAt.In(loc), e.Precision, own)
		r.End = &end
	}

	return r
}

// DayZone returns the abbreviations of a day's readings in order, joined by /, and whether they
// span more than one offset; fallback names the zone when no reading has a clock.
func DayZone(day []Reading, fallback string) (string, bool) {
	var names []string
	offsets := map[string]bool{}
	for _, r := range day {
		for _, p := range zoned(r) {
			if !slices.Contains(names, p.Abbreviation) {
				names = append(names, p.Abbreviation)
			}
			offsets[p.Offset] = true
		}
	}
	if len(names) == 0 {
		return fallback, false
	}

	return strings.Join(names, "/"), len(offsets) > 1
}

// Stamp is when an export was written, as people read it: 2006-01-02 15:04 MST.
func Stamp(at time.Time, zone *time.Location) string {
	local := at.In(zone)
	name, _ := local.Zone()

	return local.Format("2006-01-02 15:04") + " " + name
}

// zoned returns the parts of r that show a clock in the zone.
func zoned(r Reading) []Part {
	parts := []Part{r.Start}
	if r.End != nil {
		parts = append(parts, *r.End)
	}

	return slices.DeleteFunc(parts, func(p Part) bool { return p.Clock == "" || p.OwnOffset })
}

func part(t time.Time, precision timeline.Precision, own bool) Part {
	name, _ := t.Zone()

	return Part{
		Date:         t.Format(time.DateOnly),
		Clock:        clock(t, precision),
		Abbreviation: name,
		Offset:       t.Format("-07:00"),
		OwnOffset:    own,
	}
}

// clock returns t's wall clock to precision; a day has none.
func clock(t time.Time, precision timeline.Precision) string {
	layout := time.TimeOnly
	switch precision {
	case timeline.PrecisionDay:
		return ""
	case timeline.PrecisionHour:
		layout = "15:--"
	case timeline.PrecisionMinute:
		layout = "15:04"
	case timeline.PrecisionSecond:
	case timeline.PrecisionMillisecond:
		layout = "15:04:05.000"
	case timeline.PrecisionMicrosecond:
		layout = "15:04:05.000000"
	case timeline.PrecisionNanosecond:
		layout = "15:04:05.000000000"
	}

	return t.Format(layout)
}

// Location is where e's time reads: zone when e's window lines up there, else e's own offset.
func Location(e timeline.Event, zone *time.Location) *time.Location {
	if e.Precision.Truncate(e.OccurredAt.In(zone)).Equal(e.OccurredAt) {
		return zone
	}

	return ownOffset(e)
}

func ownOffset(e timeline.Event) *time.Location {
	if e.OccurredOffset == nil {
		return time.UTC
	}

	return time.FixedZone("", *e.OccurredOffset)
}
