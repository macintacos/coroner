package timeline

import (
	"slices"
	"time"
)

// Precision is how exact an event's occurred at is.
type Precision string

// Precisions, coarsest first. Their values are stored and exported verbatim.
const (
	PrecisionDay         Precision = "day"
	PrecisionHour        Precision = "hour"
	PrecisionMinute      Precision = "minute"
	PrecisionSecond      Precision = "second"
	PrecisionMillisecond Precision = "millisecond"
	PrecisionMicrosecond Precision = "microsecond"
	PrecisionNanosecond  Precision = "nanosecond"
)

// precisions lists every Precision, coarsest first.
func precisions() []Precision {
	return []Precision{
		PrecisionDay, PrecisionHour, PrecisionMinute, PrecisionSecond,
		PrecisionMillisecond, PrecisionMicrosecond, PrecisionNanosecond,
	}
}

// Valid reports whether p is a known precision.
func (p Precision) Valid() bool {
	return slices.Contains(precisions(), p)
}

// Truncate returns the start of t's precision window, measured in t's own offset and
// returned in that fixed offset rather than t's named location. An unknown precision
// returns t unchanged.
func (p Precision) Truncate(t time.Time) time.Time {
	_, offset := t.Zone()
	local := t.In(time.FixedZone("", offset))
	year, month, day := local.Date()
	hour, minute, _ := local.Clock()

	switch p {
	case PrecisionDay:
		return time.Date(year, month, day, 0, 0, 0, 0, local.Location())
	case PrecisionHour:
		return time.Date(year, month, day, hour, 0, 0, 0, local.Location())
	case PrecisionMinute:
		return time.Date(year, month, day, hour, minute, 0, 0, local.Location())
	// Offsets are whole seconds, so absolute truncation matches the wall clock here.
	case PrecisionSecond:
		return local.Truncate(time.Second)
	case PrecisionMillisecond:
		return local.Truncate(time.Millisecond)
	case PrecisionMicrosecond:
		return local.Truncate(time.Microsecond)
	case PrecisionNanosecond:
		return local
	}

	return t
}
