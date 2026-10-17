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
	// time.Parse also accepts a one-digit hour and a comma before the fraction.
	if err != nil || FormatInstant(t) != s {
		return time.Time{}, fmt.Errorf("parse instant %q: want the form YYYY-MM-DDThh:mm:ss.nnnnnnnnnZ", s)
	}

	return t, nil
}
