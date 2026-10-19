package export

import (
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

// Instant is a point in time that encodes as coroner's canonical instant: fixed-width
// RFC 3339 in UTC with nine fractional digits, so lexical order is chronological order.
type Instant time.Time

// MarshalText renders i with timeline.FormatInstant, and fails for a year outside 0000–9999,
// which the fixed width cannot hold.
func (i Instant) MarshalText() ([]byte, error) {
	err := timeline.ValidateInstantRange("", "instant", time.Time(i))
	if err != nil {
		return nil, err
	}

	return []byte(timeline.FormatInstant(time.Time(i))), nil
}

// UnmarshalText reads an instant in the canonical spelling only.
func (i *Instant) UnmarshalText(b []byte) error {
	t, err := timeline.ParseInstant(string(b))
	*i = Instant(t)

	return err
}
