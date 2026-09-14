package timeline

import (
	"errors"
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

// Validate reports every way o breaks the domain rules, or nil.
func (o Overlay) Validate() error {
	errs := []error{ValidateOverlayName(o.Name)}
	if len(o.TimelineIDs) == 0 {
		errs = append(errs, errors.New("timelines: an overlay needs at least one"))
	}

	return errors.Join(errs...)
}
