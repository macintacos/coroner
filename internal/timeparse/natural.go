package timeparse

import (
	"strings"
	"time"
	"unicode"

	dateparser "github.com/macintacos/go-dateparser"
	"github.com/macintacos/go-dateparser/date"

	"github.com/macintacos/coroner/internal/timeline"
)

const (
	coarseHint = "name a day, such as 2026-09-01"
	futureHint = "natural language reads past times; give a later time in RFC 3339"
)

// parseNatural reads natural language through go-dateparser as the latest matching time not
// after now. Unlike the other layers its results may change when the library does.
func parseNatural(text string, now time.Time, loc *time.Location) (Resolved, error) {
	// On a clock without DST, go-dateparser keeps a wall time as typed and counts durations
	// in real time.
	_, offset := now.In(loc).Zone()
	clock := now.In(time.FixedZone("", offset))

	got, err := dateparse(text, clock, dateparser.CurrentPeriod)
	if err != nil {
		return Resolved{}, err
	}

	// go-dateparser has no week period and reads a week as a day.
	if lastSpace := strings.LastIndexFunc(text, unicode.IsSpace); strings.EqualFold(text[lastSpace+1:], "week") {
		return Resolved{}, &ParseError{Err: ErrTooCoarse, Hint: coarseHint}
	}

	// A stated zone comes back as a new fixed zone, a naive input in the clock's.
	if got.Time.Location() != clock.Location() {
		return parseZoned(text, now, got.Time.Location())
	}

	// A duration from now is the same instant on any clock; a wall time moves with the offset.
	shifted, err := dateparse(text, now.In(time.FixedZone("", offset+60*60)), dateparser.CurrentPeriod)
	if err != nil {
		return Resolved{}, err
	}

	if shifted.Time.Equal(got.Time) {
		return resolveDuration(got, now, loc)
	}

	return parseWall(text, clock, now, loc)
}

func resolveDuration(got date.Date, now time.Time, loc *time.Location) (Resolved, error) {
	precision, err := datePrecision(got)
	if err != nil {
		return Resolved{}, err
	}

	if got.Time.After(now) {
		return Resolved{}, &ParseError{Err: ErrFuture, Hint: futureHint}
	}

	t := got.Time.In(loc)
	_, offset := t.Zone()

	return resolve(t, &offset, precision), nil
}

// parseWall reads a naive wall time on clock, a fixed-offset now, and resolves it in loc.
func parseWall(text string, clock, now time.Time, loc *time.Location) (Resolved, error) {
	got, err := latestPast(text, clock, now)
	if err != nil {
		return Resolved{}, err
	}

	precision, err := datePrecision(got)
	if err != nil {
		return Resolved{}, err
	}

	wall := wallClock(got.Time)
	if precision == timeline.PrecisionDay {
		return resolveDay(wall, loc), nil
	}

	return resolveLocal(wall, loc, precision)
}

// resolveDay resolves wall's day to its first instant in loc: its 00:00, read in the offset
// before a DST change at midnight, which must not reject a date that never stated a time.
func resolveDay(wall wallTime, loc *time.Location) Resolved {
	year, month, day := wall.t.Date()
	midnight := wallTime{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}

	t, err := resolveWall(midnight, loc)
	if err != nil {
		t = readInOffsetAt(midnight, loc, -24*time.Hour)
	}

	_, offset := t.Zone()

	return resolve(t, &offset, timeline.PrecisionDay)
}

// parseZoned reads an input that stated zone entirely in that zone.
func parseZoned(text string, now time.Time, zone *time.Location) (Resolved, error) {
	got, err := latestPast(text, now.In(zone), now)
	if err != nil {
		return Resolved{}, err
	}

	precision, err := datePrecision(got)
	if err != nil {
		return Resolved{}, err
	}

	// A stated UTC says nothing about the local offset.
	var offset *int
	if name, seconds := got.Time.Zone(); name != "UTC" && name != "Z" {
		offset = &seconds
	}

	return resolve(got.Time, offset, precision), nil
}

// latestPast reads text on clock as the latest time it can name that is not after now.
//
// go-dateparser's Past and Future preferences misdate a zoned time of day, comparing it with
// CurrentTime as though that were UTC (internal/parser/absolute/parser.go:549-561 in
// v1.4.4), and Past moves a time of day on the 1st into the next month. So the two only
// show whether the input can move at all, and a time of day moves back by parsing it on
// yesterday's clock.
func latestPast(text string, clock, now time.Time) (date.Date, error) {
	current, err := dateparse(text, clock, dateparser.CurrentPeriod)
	if err != nil || !current.Time.After(now) {
		return current, err
	}

	past, err := dateparse(text, clock, dateparser.Past)
	if err != nil {
		return date.Date{}, err
	}

	future, err := dateparse(text, clock, dateparser.Future)
	if err != nil {
		return date.Date{}, err
	}

	// Neither preference moves an input that names a future time outright, such as tomorrow.
	if past.Time.Equal(current.Time) && future.Time.Equal(current.Time) {
		return date.Date{}, &ParseError{Err: ErrFuture, Hint: futureHint}
	}

	dayEarlier, err := dateparse(text, clock.AddDate(0, 0, -1), dateparser.CurrentPeriod)
	if err != nil {
		return date.Date{}, err
	}

	if dayEarlier.Time.Equal(current.Time.AddDate(0, 0, -1)) && !dayEarlier.Time.After(now) {
		return dayEarlier, nil
	}

	if !past.Time.After(now) {
		return past, nil
	}

	return date.Date{}, &ParseError{Err: ErrFuture, Hint: futureHint}
}

func dateparse(text string, clock time.Time, source dateparser.PreferredDateSource) (date.Date, error) {
	got, err := dateparser.Parse(&dateparser.Configuration{
		Languages:           []string{"en"},
		CurrentTime:         clock,
		PreferredDateSource: source,
		ReturnTimeAsPeriod:  true,
	}, text)
	if err != nil {
		return date.Date{}, errNoMatch
	}

	return got, nil
}

// datePrecision is got's period as a precision; a second keeps any fraction go-dateparser read
// at the coarsest sub-second precision that holds it.
func datePrecision(got date.Date) (timeline.Precision, error) {
	switch got.Period {
	case date.Second:
		return subsecondPrecision(got.Time.Nanosecond()), nil
	case date.Minute:
		return timeline.PrecisionMinute, nil
	case date.Hour:
		return timeline.PrecisionHour, nil
	case date.Day:
		return timeline.PrecisionDay, nil
	case date.Month, date.Year:
		return "", &ParseError{Err: ErrTooCoarse, Hint: coarseHint}
	case date.None:
	}

	return "", errNoMatch
}

// subsecondPrecision is the coarsest precision that keeps nanos, the fraction of a second;
// the text's digits cannot say, as a date such as 12.09.2026 has a dot too.
func subsecondPrecision(nanos int) timeline.Precision {
	switch {
	case nanos == 0:
		return timeline.PrecisionSecond
	case nanos%int(time.Millisecond) == 0:
		return timeline.PrecisionMillisecond
	case nanos%int(time.Microsecond) == 0:
		return timeline.PrecisionMicrosecond
	default:
		return timeline.PrecisionNanosecond
	}
}
