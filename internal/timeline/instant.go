package timeline

import (
	"fmt"
	"time"
)

const instantLayout = "2006-01-02T15:04:05.000000000Z"

// FormatInstant renders t as coroner's canonical instant: UTC, nine fractional digits,
// 30 characters for years 0000–9999. It is the only function that may produce the
// format; fixed width is what makes lexical order equal chronological order.
func FormatInstant(t time.Time) string {
	return t.UTC().Format(instantLayout)
}

// ParseInstant reads an instant produced by FormatInstant and rejects any other spelling.
func ParseInstant(s string) (time.Time, error) {
	t, err := time.Parse(instantLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse instant %q: %w", s, err)
	}

	// time.Parse accepts one-digit hours, minutes and seconds.
	if FormatInstant(t) != s {
		return time.Time{}, fmt.Errorf("parse instant %q: not in the form %s", s, instantLayout)
	}

	return t, nil
}

// Truncate returns the start of t's precision window, measured in t's own location.
func (p Precision) Truncate(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, minute, _ := t.Clock()

	switch p {
	case PrecisionDay:
		return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
	case PrecisionHour:
		return time.Date(year, month, day, hour, 0, 0, 0, t.Location())
	case PrecisionMinute:
		return time.Date(year, month, day, hour, minute, 0, 0, t.Location())
	// Offsets are whole seconds, so absolute truncation matches the wall clock here.
	case PrecisionSecond:
		return t.Truncate(time.Second)
	case PrecisionMillisecond:
		return t.Truncate(time.Millisecond)
	case PrecisionMicrosecond:
		return t.Truncate(time.Microsecond)
	case PrecisionNanosecond:
		return t
	}

	return t
}
