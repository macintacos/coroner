// Package timeparse resolves the time a person or agent typed into an instant, the offset
// it stated, its precision and whether it was approximate.
package timeparse

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

const digits = "0123456789"

// Resolved is a typed time resolved to an instant.
type Resolved struct {
	// Instant is the start of its Precision window, measured in the offset it was observed in.
	Instant time.Time
	// Offset is seconds east of UTC; nil when the input was Z, UTC or an epoch, which say
	// nothing about the local offset.
	Offset      *int
	Precision   timeline.Precision
	Approximate bool
}

// Kinds of rejected input, matched with [errors.Is].
var (
	ErrUnrecognized = errors.New("not a time coroner recognizes")
	ErrDSTGap       = errors.New("that wall-clock time does not exist in the time zone")
	ErrDSTFold      = errors.New("that wall-clock time happens twice in the time zone")
)

// ParseError is a rejected input. Err is one of the Err kinds; Hint says how to fix the input.
type ParseError struct {
	Input, Hint string
	Err         error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("time %q: %v", e.Input, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

type layer func(text string, now time.Time, loc *time.Location) (Resolved, error)

// Parse resolves input as of now. A naive input is read in loc; an input that states an
// offset or zone is read in that.
func Parse(input string, now time.Time, loc *time.Location) (Resolved, error) {
	text, approximate := cutApproximate(strings.TrimSpace(input))

	for _, parse := range []layer{parseEpoch, parseLayout, parseNaive, parseSyslog} {
		got, err := parse(text, now, loc)
		rejected, ok := errors.AsType[*ParseError](err)
		if ok {
			rejected.Input = input
			return Resolved{}, rejected
		}

		// A bare ErrUnrecognized passes the input to the next layer.
		if err != nil {
			continue
		}

		got.Approximate = approximate

		return got, nil
	}

	return Resolved{}, &ParseError{
		Input: input, Err: ErrUnrecognized, Hint: "use RFC 3339, such as 2026-09-12T14:05:03Z",
	}
}

func cutApproximate(text string) (string, bool) {
	if rest, ok := strings.CutPrefix(text, "~"); ok {
		return strings.TrimSpace(rest), true
	}

	if len(text) > len("around ") && strings.EqualFold(text[:len("around ")], "around ") {
		return strings.TrimSpace(text[len("around "):]), true
	}

	return text, false
}

// parseEpoch reads an all-digit epoch whose unit its digit count gives: 10 seconds, 13
// milliseconds, 16 microseconds or 19 nanoseconds.
func parseEpoch(text string, _ time.Time, _ *time.Location) (Resolved, error) {
	subsecondDigits := len(text) - 10
	if subsecondDigits%3 != 0 || subsecondDigits < 0 || subsecondDigits > 9 || strings.Trim(text, digits) != "" {
		return Resolved{}, ErrUnrecognized
	}

	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return Resolved{}, &ParseError{Err: ErrUnrecognized, Hint: "an epoch in nanoseconds must fit in 64 bits"}
	}

	unit := int64(1)
	for range subsecondDigits {
		unit *= 10
	}

	instant := time.Unix(n/unit, n%unit*(int64(time.Second)/unit)).UTC()

	return resolve(instant, nil, precisionOf(subsecondDigits)), nil
}

// parseLayout reads the standard library layouts. time.Parse accepts a fractional second
// after the seconds field, with a dot or a comma, even where the layout shows none.
func parseLayout(text string, _ time.Time, _ *time.Location) (Resolved, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-0700"} {
		t, err := time.ParseInLocation(layout, text, time.UTC)
		if err != nil {
			continue
		}

		var offset *int
		// ParseInLocation reads +00:00 as time.UTC too, so only the text tells Z apart.
		if !strings.HasSuffix(text, "Z") {
			_, seconds := t.Zone()
			offset = &seconds
		}

		return resolve(t, offset, precisionOf(fractionDigits(text))), nil
	}

	return Resolved{}, ErrUnrecognized
}

// parseNaive reads a date and time with no offset as a wall clock in loc.
func parseNaive(text string, _ time.Time, loc *time.Location) (Resolved, error) {
	wall, err := time.ParseInLocation(time.DateTime, text, time.UTC)
	if err != nil {
		return Resolved{}, ErrUnrecognized
	}

	return resolveLocal(wall, loc, precisionOf(fractionDigits(text)))
}

// parseSyslog reads a yearless syslog stamp as a wall clock in loc, in the latest year that
// puts it on a real date no later than now.
func parseSyslog(text string, now time.Time, loc *time.Location) (Resolved, error) {
	stamp, err := time.ParseInLocation(time.Stamp, text, time.UTC)
	if err != nil {
		return Resolved{}, ErrUnrecognized
	}

	nowWall := wallClock(now.In(loc))
	for year := nowWall.Year(); ; year-- {
		wall := time.Date(year, stamp.Month(), stamp.Day(),
			stamp.Hour(), stamp.Minute(), stamp.Second(), stamp.Nanosecond(), time.UTC)
		// time.Date moves February 29 to March 1 outside a leap year.
		if wall.Day() == stamp.Day() && !wall.After(nowWall) {
			return resolveLocal(wall, loc, precisionOf(fractionDigits(text)))
		}
	}
}

func resolveLocal(wall time.Time, loc *time.Location, precision timeline.Precision) (Resolved, error) {
	t, err := resolveWall(wall, loc)
	if err != nil {
		return Resolved{}, err
	}

	_, offset := t.Zone()

	return resolve(t, &offset, precision), nil
}

func fractionDigits(text string) int {
	i := strings.IndexAny(text, ".,")
	if i < 0 {
		return 0
	}

	fraction := text[i+1:]

	return len(fraction) - len(strings.TrimLeft(fraction, digits))
}

func precisionOf(subsecondDigits int) timeline.Precision {
	switch {
	case subsecondDigits == 0:
		return timeline.PrecisionSecond
	case subsecondDigits <= 3:
		return timeline.PrecisionMillisecond
	case subsecondDigits <= 6:
		return timeline.PrecisionMicrosecond
	default:
		return timeline.PrecisionNanosecond
	}
}

func resolve(t time.Time, offset *int, precision timeline.Precision) Resolved {
	return Resolved{Instant: precision.Truncate(t), Offset: offset, Precision: precision}
}
