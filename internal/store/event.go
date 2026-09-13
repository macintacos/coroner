package store

import (
	"context"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// AddEvent adds e to its timeline under the timeline's next seq and returns e with that
// Seq. When no source is called e.Source yet, it creates one with id newSourceID.
func (s *Store) AddEvent(ctx context.Context, e timeline.Event, newSourceID string) (timeline.Event, error) {
	var seq int64
	err := s.write(ctx, func(q *sqlcgen.Queries) error {
		var err error
		seq, err = q.AllocateSeq(ctx, e.TimelineID)
		if err != nil {
			return err
		}

		source, err := sourceID(ctx, q, e.Source, newSourceID, e.CreatedAt)
		if err != nil {
			return err
		}

		_, err = q.CreateEvent(ctx, sqlcgen.CreateEventParams{
			ID:                e.ID,
			TimelineID:        e.TimelineID,
			Seq:               seq,
			OccurredAt:        timeline.FormatInstant(e.OccurredAt),
			OccurredOffset:    offsetParam(e.OccurredOffset),
			OccurredPrecision: string(e.Precision),
			Approximate:       sqliteBool(e.Approximate),
			OccurredRaw:       e.OccurredRaw,
			EndedAt:           formatOptional(e.EndedAt),
			SourceID:          source,
			Text:              e.Text,
			Url:               e.URL,
			Author:            e.Author,
			CreatedAt:         timeline.FormatInstant(e.CreatedAt),
			UpdatedAt:         timeline.FormatInstant(e.UpdatedAt),
		})
		if err != nil {
			return err
		}

		return addTags(ctx, q, e.ID, e.Tags)
	})
	if err != nil {
		return timeline.Event{}, err
	}

	e.Seq = seq
	return e, nil
}

// Event returns the event with id.
func (s *Store) Event(ctx context.Context, id string) (timeline.Event, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Event, error) {
		row, err := q.GetEvent(ctx, id)
		if err != nil {
			return timeline.Event{}, err
		}

		return loadEvent(ctx, q, row)
	})
}

// EventBySeq returns the event numbered seq in the timeline with timelineID.
func (s *Store) EventBySeq(ctx context.Context, timelineID string, seq int64) (timeline.Event, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Event, error) {
		row, err := q.GetEventBySeq(ctx, sqlcgen.GetEventBySeqParams{TimelineID: timelineID, Seq: seq})
		if err != nil {
			return timeline.Event{}, err
		}

		return loadEvent(ctx, q, row)
	})
}

// Events returns the events of the timeline with timelineID, ordered by occurred at, then
// seq.
func (s *Store) Events(ctx context.Context, timelineID string) ([]timeline.Event, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Event, error) {
		rows, err := q.ListEvents(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Event) (timeline.Event, error) { return loadEvent(ctx, q, r) })
	})
}

// UpdateEvent stores every field of e but its timeline, seq and created at, and replaces
// its tags with e.Tags. When no source is called e.Source yet, it creates one with id
// newSourceID.
func (s *Store) UpdateEvent(ctx context.Context, e timeline.Event, newSourceID string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		source, err := sourceID(ctx, q, e.Source, newSourceID, e.UpdatedAt)
		if err != nil {
			return err
		}

		_, err = q.UpdateEvent(ctx, sqlcgen.UpdateEventParams{
			OccurredAt:        timeline.FormatInstant(e.OccurredAt),
			OccurredOffset:    offsetParam(e.OccurredOffset),
			OccurredPrecision: string(e.Precision),
			Approximate:       sqliteBool(e.Approximate),
			OccurredRaw:       e.OccurredRaw,
			EndedAt:           formatOptional(e.EndedAt),
			SourceID:          source,
			Text:              e.Text,
			Url:               e.URL,
			Author:            e.Author,
			UpdatedAt:         timeline.FormatInstant(e.UpdatedAt),
			ID:                e.ID,
		})
		if err != nil {
			return err
		}

		tags, err := q.ListEventTags(ctx, e.ID)
		if err != nil {
			return err
		}
		for _, tag := range tags {
			err = q.DeleteEventTag(ctx, sqlcgen.DeleteEventTagParams{EventID: e.ID, Tag: tag})
			if err != nil {
				return err
			}
		}

		return addTags(ctx, q, e.ID, e.Tags)
	})
}

// DeleteEvent deletes the event with id, with its tags and annotations. Its seq is never
// given to another event.
func (s *Store) DeleteEvent(ctx context.Context, id string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.GetEvent(ctx, id)
		if err != nil {
			return err
		}

		return q.DeleteEvent(ctx, id)
	})
}

func loadEvent(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Event) (timeline.Event, error) {
	tags, err := q.ListEventTags(ctx, row.ID)
	if err != nil {
		return timeline.Event{}, err
	}
	source, err := sourceName(ctx, q, row.SourceID)
	if err != nil {
		return timeline.Event{}, err
	}

	return eventFromRow(row, source, tags)
}

// eventFromRow maps row, with the name of its source and its tags, to a timeline.Event.
func eventFromRow(row sqlcgen.Event, source *string, tags []string) (timeline.Event, error) {
	var p instants
	e := timeline.Event{
		ID:             row.ID,
		TimelineID:     row.TimelineID,
		Seq:            row.Seq,
		OccurredAt:     p.parse(row.OccurredAt),
		OccurredOffset: offsetField(row.OccurredOffset),
		Precision:      timeline.Precision(row.OccurredPrecision),
		Approximate:    row.Approximate == 1,
		OccurredRaw:    row.OccurredRaw,
		EndedAt:        p.parseOptional(row.EndedAt),
		Source:         source,
		Tags:           tags,
		Text:           row.Text,
		URL:            row.Url,
		Author:         row.Author,
		CreatedAt:      p.parse(row.CreatedAt),
		UpdatedAt:      p.parse(row.UpdatedAt),
	}

	return e, p.err
}

func addTags(ctx context.Context, q *sqlcgen.Queries, eventID string, tags []string) error {
	for _, tag := range tags {
		err := q.AddEventTag(ctx, sqlcgen.AddEventTagParams{EventID: eventID, Tag: tag})
		if err != nil {
			return err
		}
	}

	return nil
}

func offsetParam(offset *int) *int64 {
	if offset == nil {
		return nil
	}

	return new(int64(*offset))
}

func offsetField(offset *int64) *int {
	if offset == nil {
		return nil
	}

	return new(int(*offset))
}

func sqliteBool(b bool) int64 {
	if b {
		return 1
	}

	return 0
}
