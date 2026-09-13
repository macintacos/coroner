package export

import (
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

// Instant is a point in time that encodes as coroner's canonical instant: fixed-width
// RFC 3339 in UTC with nine fractional digits, so lexical order is chronological order.
type Instant time.Time

// MarshalText renders i with timeline.FormatInstant.
func (i Instant) MarshalText() ([]byte, error) {
	return []byte(timeline.FormatInstant(time.Time(i))), nil
}
