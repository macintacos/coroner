package timeline

import "time"

// Gap is a stretch of time no event covers, lasting Duration and ending at the occurred at
// of the event at index Before.
type Gap struct {
	Before   int
	Duration time.Duration
}

// Gaps returns the gaps of at least threshold between events, which must be in
// CompareEvents order. A gap runs from the latest time an earlier event covers, its ended
// at or else its occurred at, to the next event's occurred at.
func Gaps(events []Event, threshold time.Duration) []Gap {
	var gaps []Gap
	var covered time.Time
	for i, e := range events {
		if d := e.OccurredAt.Sub(covered); i > 0 && d >= threshold {
			gaps = append(gaps, Gap{Before: i, Duration: d})
		}
		if i == 0 || e.OccurredAt.After(covered) {
			covered = e.OccurredAt
		}
		if e.EndedAt != nil && e.EndedAt.After(covered) {
			covered = *e.EndedAt
		}
	}

	return gaps
}
