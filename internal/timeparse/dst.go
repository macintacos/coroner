package timeparse

import (
	"fmt"
	"slices"
	"time"
)

// wallTime is a wall-clock reading, held as a time in time.UTC, and never an instant.
type wallTime struct{ t time.Time }

// resolveWall finds the instant whose wall clock in loc reads wall. It rejects a wall clock
// that a DST change skips or repeats, where time.Date would silently pick one.
func resolveWall(wall wallTime, loc *time.Location) (time.Time, error) {
	var candidates, found []time.Time

	for _, probe := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		candidate := readInOffsetAt(wall, loc, probe)
		candidates = append(candidates, candidate)

		if wallClock(candidate.In(loc)).t.Equal(wall.t) && !slices.ContainsFunc(found, candidate.Equal) {
			found = append(found, candidate)
		}
	}

	if len(found) == 1 {
		return found[0], nil
	}

	kind, happens := ErrDSTGap, "is skipped in"
	if len(found) > 1 {
		kind, happens = ErrDSTFold, "happens twice in"
	}

	const layout = "2006-01-02T15:04:05.999999999-07:00"

	return time.Time{}, &ParseError{Err: kind, Hint: fmt.Sprintf("%s %s %s; state the offset you mean, such as %s or %s",
		wall.t.Format("2006-01-02 15:04:05.999999999"), happens, loc,
		candidates[0].Format(layout), candidates[1].Format(layout))}
}

// readInOffsetAt reads wall in the offset loc has probe away from it.
func readInOffsetAt(wall wallTime, loc *time.Location, probe time.Duration) time.Time {
	_, offset := wall.t.Add(probe).In(loc).Zone()

	return wall.t.Add(-time.Duration(offset) * time.Second).In(time.FixedZone("", offset))
}

// wallClock reads t's wall clock in t's own location.
func wallClock(t time.Time) wallTime {
	year, month, day := t.Date()
	hour, minute, second := t.Clock()

	return wallTime{time.Date(year, month, day, hour, minute, second, t.Nanosecond(), time.UTC)}
}
