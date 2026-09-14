package timeline

import (
	"errors"
	"fmt"
	"time"
)

// LinkKind says how the event a link leaves relates to the event it reaches.
type LinkKind string

// Link kinds. Their values are stored and exported verbatim.
const (
	// LinkLedTo means the from event led to the to event.
	LinkLedTo LinkKind = "led_to"
	// LinkSameAs means both events record the same moment, as two sources saw it.
	LinkSameAs LinkKind = "same_as"
	// LinkCopiedFrom means the from event was copied from the to event.
	LinkCopiedFrom LinkKind = "copied_from"
)

func (k LinkKind) valid() bool {
	switch k {
	case LinkLedTo, LinkSameAs, LinkCopiedFrom:
		return true
	}

	return false
}

// Link is a directed, typed connection from one event to another, in the same timeline or
// another.
type Link struct {
	ID          string
	FromEventID string
	ToEventID   string
	Kind        LinkKind
	CreatedAt   time.Time
	// Author is the person who added the link; nil when unknown.
	Author *string
}

// Validate reports every way l breaks the domain rules, or nil.
func (l Link) Validate() error {
	var errs []error
	if !l.Kind.valid() {
		errs = append(errs, fmt.Errorf("link kind %q: must be %q, %q or %q", l.Kind, LinkLedTo, LinkSameAs, LinkCopiedFrom))
	}
	if l.FromEventID == l.ToEventID {
		errs = append(errs, errors.New("link: an event cannot link to itself"))
	}

	return errors.Join(errs...)
}
