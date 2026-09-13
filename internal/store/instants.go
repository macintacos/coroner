package store

import (
	"cmp"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

// instants parses a row's stored instants, keeping the first error so a mapping can check
// once after reading every column.
type instants struct {
	err error
}

func (p *instants) parse(s string) time.Time {
	t, err := timeline.ParseInstant(s)
	p.err = cmp.Or(p.err, err)
	return t
}
