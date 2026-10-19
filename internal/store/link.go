package store

import (
	"context"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// AddLink adds l. It returns an error wrapping ErrConflict when a link of l's kind already
// leads from l's from event to its to event.
func (s *Store) AddLink(ctx context.Context, l timeline.Link) error {
	return s.change(ctx, Commit, nil, func(tx *Tx) error { return tx.AddLink(ctx, l) })
}

// AddLink adds l. It returns an error wrapping ErrConflict when a link of l's kind already
// leads from l's from event to its to event.
func (tx *Tx) AddLink(ctx context.Context, l timeline.Link) error {
	err := l.Validate()
	if err != nil {
		return err
	}

	return translate(createLink(ctx, tx.q, l))
}

// Link returns the link with id.
func (s *Store) Link(ctx context.Context, id string) (timeline.Link, error) {
	return readIn(ctx, s, func(tx *Tx) (timeline.Link, error) { return tx.Link(ctx, id) })
}

// Link returns the link with id.
func (tx *Tx) Link(ctx context.Context, id string) (timeline.Link, error) {
	return translated(func() (timeline.Link, error) {
		row, err := tx.q.GetLink(ctx, id)
		if err != nil {
			return timeline.Link{}, err
		}

		return linkFromRow(row)
	})
}

// LinkBetween returns the link of kind from the event with fromEventID to the event with
// toEventID.
func (s *Store) LinkBetween(ctx context.Context, fromEventID, toEventID string, kind timeline.LinkKind) (timeline.Link, error) {
	return readIn(ctx, s, func(tx *Tx) (timeline.Link, error) { return tx.LinkBetween(ctx, fromEventID, toEventID, kind) })
}

// LinkBetween returns the link of kind from the event with fromEventID to the event with
// toEventID.
func (tx *Tx) LinkBetween(ctx context.Context, fromEventID, toEventID string, kind timeline.LinkKind) (timeline.Link, error) {
	return translated(func() (timeline.Link, error) {
		row, err := tx.q.GetLinkBetween(ctx, sqlcgen.GetLinkBetweenParams{FromEventID: fromEventID, ToEventID: toEventID, Kind: string(kind)})
		if err != nil {
			return timeline.Link{}, err
		}

		return linkFromRow(row)
	})
}

// EventLinks returns the links from and to the event with eventID, oldest first.
func (s *Store) EventLinks(ctx context.Context, eventID string) ([]timeline.Link, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Link, error) { return tx.EventLinks(ctx, eventID) })
}

// EventLinks returns the links from and to the event with eventID, oldest first.
func (tx *Tx) EventLinks(ctx context.Context, eventID string) ([]timeline.Link, error) {
	return translated(func() ([]timeline.Link, error) {
		rows, err := tx.q.ListEventLinks(ctx, eventID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, linkFromRow)
	})
}

// TimelineLinks returns the links from events in the timeline with timelineID, to events in
// any timeline, oldest first. A link between two timelines is listed only under its from
// event's timeline, so reading several timelines lists it once.
func (s *Store) TimelineLinks(ctx context.Context, timelineID string) ([]timeline.Link, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Link, error) { return tx.TimelineLinks(ctx, timelineID) })
}

// TimelineLinks returns the links from events in the timeline with timelineID, to events in
// any timeline, oldest first. A link between two timelines is listed only under its from
// event's timeline, so reading several timelines lists it once.
func (tx *Tx) TimelineLinks(ctx context.Context, timelineID string) ([]timeline.Link, error) {
	return translated(func() ([]timeline.Link, error) {
		rows, err := tx.q.ListTimelineLinks(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, linkFromRow)
	})
}

// DeleteLink deletes the link with id.
func (s *Store) DeleteLink(ctx context.Context, id string) error {
	return s.change(ctx, Commit, nil, func(tx *Tx) error { return tx.DeleteLink(ctx, id) })
}

// DeleteLink deletes the link with id.
func (tx *Tx) DeleteLink(ctx context.Context, id string) error {
	_, err := tx.q.GetLink(ctx, id)
	if err != nil {
		return translate(err)
	}

	return translate(tx.q.DeleteLink(ctx, id))
}

func createLink(ctx context.Context, q *sqlcgen.Queries, l timeline.Link) error {
	_, err := q.CreateLink(ctx, sqlcgen.CreateLinkParams{
		ID:          l.ID,
		FromEventID: l.FromEventID,
		ToEventID:   l.ToEventID,
		Kind:        string(l.Kind),
		CreatedAt:   timeline.FormatInstant(l.CreatedAt),
		Author:      l.Author,
	})
	return err
}

func linkFromRow(row sqlcgen.EventLink) (timeline.Link, error) {
	var p instants
	l := timeline.Link{
		ID:          row.ID,
		FromEventID: row.FromEventID,
		ToEventID:   row.ToEventID,
		Kind:        timeline.LinkKind(row.Kind),
		CreatedAt:   p.parse(row.CreatedAt),
		Author:      row.Author,
	}

	return l, p.err
}
