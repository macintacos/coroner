package clikit

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// NewTimelineJSON returns t as --json prints it: its export object.
func NewTimelineJSON(t timeline.Timeline) export.Timeline {
	return export.NewTimeline(t)
}

// FindTimeline returns the timeline called nameOrID, or else the one with that id. A name
// that is another timeline's id is ambiguous.
func FindTimeline(ctx context.Context, s Records, nameOrID string) (timeline.Timeline, error) {
	t, err := s.TimelineByName(ctx, nameOrID)
	if errors.Is(err, store.ErrNotFound) {
		t, err = s.Timeline(ctx, nameOrID)
	} else if err == nil {
		err = checkUnambiguous(ctx, s, t, nameOrID)
	}
	if errors.Is(err, store.ErrNotFound) {
		return t, &Error{
			Kind:    KindTimelineNotFound,
			Message: fmt.Sprintf("no timeline is named %q or has that id", nameOrID),
			Hint:    "Run 'coroner timeline list' to see the timelines.",
			Err:     err,
		}
	}

	return t, err
}

// checkUnambiguous returns ambiguous_id when a timeline other than named has nameOrID as its
// id.
func checkUnambiguous(ctx context.Context, s Records, named timeline.Timeline, nameOrID string) error {
	byID, err := s.Timeline(ctx, nameOrID)
	switch {
	case errors.Is(err, store.ErrNotFound) || (err == nil && byID.ID == named.ID):
		return nil
	case err != nil:
		return err
	}

	return &Error{
		Kind:    KindAmbiguousID,
		Message: fmt.Sprintf("`%s` is the name of timeline %s and the id of timeline `%s`", nameOrID, named.ID, byID.Name),
		Hint:    fmt.Sprintf("Pass %s for the first, or `%s` for the second.", named.ID, byID.Name),
	}
}

// TimelineIDs returns the timelines' ids, in order.
func TimelineIDs(timelines []timeline.Timeline) []string {
	ids := make([]string, 0, len(timelines))
	for _, t := range timelines {
		ids = append(ids, t.ID)
	}

	return ids
}

// ReadEach returns what read returns for the id of each of timelines, in order.
func ReadEach[T any](timelines []timeline.Timeline, read func(timelineID string) ([]T, error)) ([]T, error) {
	var all []T
	for _, t := range timelines {
		records, err := read(t.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
	}

	return all, nil
}

// AppendUnique appends each value in add that values does not hold yet.
func AppendUnique(values []string, add ...string) []string {
	for _, value := range add {
		if !slices.Contains(values, value) {
			values = append(values, value)
		}
	}

	return values
}

// stringArray returns the values of the repeatable flag called name as given: pflag's
// GetStringArray round-trips them through CSV, which drops a lone empty value.
func stringArray(flags *pflag.FlagSet, name string) []string {
	values, _ := flags.Lookup(name).Value.(pflag.SliceValue)
	return values.GetSlice()
}

// TimelinesByID returns the timeline with each of ids, in their order.
func TimelinesByID(ctx context.Context, s Records, ids []string) ([]timeline.Timeline, error) {
	timelines := make([]timeline.Timeline, 0, len(ids))
	for _, id := range ids {
		t, err := s.Timeline(ctx, id)
		if err != nil {
			return nil, err
		}
		timelines = append(timelines, t)
	}

	return timelines, nil
}

// NamesOf returns the timelines' names, in order.
func NamesOf(timelines []timeline.Timeline) []string {
	names := make([]string, 0, len(timelines))
	for _, t := range timelines {
		names = append(names, t.Name)
	}

	return names
}
