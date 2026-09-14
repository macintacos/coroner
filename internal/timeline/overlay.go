package timeline

import (
	"time"
)

// Overlay is a saved set of timelines, read together as one chronology. It is local state,
// like the current timeline, and never exported.
type Overlay struct {
	ID string
	// Name is the grammar-checked name --overlay selects it by; see ValidateOverlayName.
	Name string
	// TimelineIDs are the overlay's timelines, in the order they were given.
	TimelineIDs []string
	CreatedAt   time.Time
}
