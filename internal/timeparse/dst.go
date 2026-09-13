package timeparse

import (
	"slices"
	"strings"
	"time"
)

// resolveWall finds the instant whose wall clock in loc reads wall, a time in time.UTC. It
// rejects a wall clock that a DST change skips or repeats, where time.Date would silently
// pick one.
func resolveWall(wall time.Time, loc *time.Location) (time.Time, error) {
	var found []time.Time
	var stated []string

	for _, probe := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		_, offset := wall.Add(probe).In(loc).Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second).In(time.FixedZone("", offset))
		stated = append(stated, candidate.Format(time.RFC3339Nano))

		if wallClock(candidate.In(loc)).Equal(wall) && !slices.ContainsFunc(found, candidate.Equal) {
			found = append(found, candidate)
		}
	}

	hint := "state the offset you mean, such as " + strings.Join(stated, " or ")

	switch len(found) {
	case 0:
		return time.Time{}, &ParseError{Err: ErrDSTGap, Hint: hint}
	case 1:
		return found[0], nil
	default:
		return time.Time{}, &ParseError{Err: ErrDSTFold, Hint: hint}
	}
}

// wallClock re-expresses t's wall-clock reading in time.UTC.
func wallClock(t time.Time) time.Time {
	year, month, day := t.Date()
	hour, minute, second := t.Clock()

	return time.Date(year, month, day, hour, minute, second, t.Nanosecond(), time.UTC)
}
