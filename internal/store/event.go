package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// AddEvent adds e to its timeline under the timeline's next seq, with attachments, and
// returns e with that Seq. When no source is called e.Source yet, it creates one with id
// newSourceID.
func (s *Store) AddEvent(
	ctx context.Context, e timeline.Event, newSourceID string, attachments ...NewAttachment,
) (timeline.Event, error) {
	var added timeline.Event
	_, err := s.Change(ctx, ChangeOptions{Mode: Commit, Attach: attachments}, func(tx *Tx) error {
		var err error
		added, err = tx.AddEvent(ctx, e, newSourceID)
		if err != nil {
			return err
		}
		tx.Attach(timeline.AttachmentOwner{EventID: new(e.ID)})
		return nil
	})
	if err != nil {
		return timeline.Event{}, err
	}

	return added, nil
}

// AddEvent adds e to its timeline under the timeline's next seq and returns e with that
// Seq. When no source is called e.Source yet, it creates one with id newSourceID.
func (tx *Tx) AddEvent(ctx context.Context, e timeline.Event, newSourceID string) (timeline.Event, error) {
	err := e.Validate()
	if err != nil {
		return timeline.Event{}, err
	}

	return translated(func() (timeline.Event, error) {
		seq, err := tx.q.AllocateSeq(ctx, e.TimelineID)
		if err != nil {
			return timeline.Event{}, err
		}

		source, err := sourceID(ctx, tx.q, e.Source, newSourceID, e.CreatedAt)
		if err != nil {
			return timeline.Event{}, err
		}

		e.Seq = seq
		return e, createEvent(ctx, tx.q, e, seq, source)
	})
}

// Event returns the event with id.
func (s *Store) Event(ctx context.Context, id string) (timeline.Event, error) {
	return readIn(ctx, s, func(tx *Tx) (timeline.Event, error) { return tx.Event(ctx, id) })
}

// Event returns the event with id.
func (tx *Tx) Event(ctx context.Context, id string) (timeline.Event, error) {
	return translated(func() (timeline.Event, error) {
		row, err := tx.q.GetEvent(ctx, id)
		if err != nil {
			return timeline.Event{}, err
		}

		return loadEvent(ctx, tx.q, row)
	})
}

// EventBySeq returns the event numbered seq in the timeline with timelineID.
func (s *Store) EventBySeq(ctx context.Context, timelineID string, seq int64) (timeline.Event, error) {
	return readIn(ctx, s, func(tx *Tx) (timeline.Event, error) { return tx.EventBySeq(ctx, timelineID, seq) })
}

// EventBySeq returns the event numbered seq in the timeline with timelineID.
func (tx *Tx) EventBySeq(ctx context.Context, timelineID string, seq int64) (timeline.Event, error) {
	return translated(func() (timeline.Event, error) {
		row, err := tx.q.GetEventBySeq(ctx, sqlcgen.GetEventBySeqParams{TimelineID: timelineID, Seq: seq})
		if err != nil {
			return timeline.Event{}, err
		}

		return loadEvent(ctx, tx.q, row)
	})
}

// EventsByIDPrefix returns up to limit events, in any timeline, whose id starts with
// prefix, ordered by id. prefix is matched with GLOB, so it must be non-empty lowercase hex
// and dashes: *, ? and [ are patterns, and ids are lowercase.
func (s *Store) EventsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Event, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Event, error) { return tx.EventsByIDPrefix(ctx, prefix, limit) })
}

// EventsByIDPrefix returns up to limit events, in any timeline, whose id starts with
// prefix, ordered by id. prefix is matched with GLOB, so it must be non-empty lowercase hex
// and dashes: *, ? and [ are patterns, and ids are lowercase.
func (tx *Tx) EventsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Event, error) {
	if prefix == "" || strings.Trim(prefix, "0123456789abcdef-") != "" {
		return nil, fmt.Errorf("event id prefix %q is not lowercase hex and dashes", prefix)
	}

	return translated(func() ([]timeline.Event, error) {
		rows, err := tx.q.ListEventsByIDPrefix(ctx, sqlcgen.ListEventsByIDPrefixParams{ID: prefix + "*", Limit: int64(limit)})
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Event) (timeline.Event, error) { return loadEvent(ctx, tx.q, r) })
	})
}

// Events returns the events of the timeline with timelineID, ordered by occurred at, then
// seq.
func (s *Store) Events(ctx context.Context, timelineID string) ([]timeline.Event, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Event, error) { return tx.Events(ctx, timelineID) })
}

// Events returns the events of the timeline with timelineID, ordered by occurred at, then
// seq.
func (tx *Tx) Events(ctx context.Context, timelineID string) ([]timeline.Event, error) {
	return translated(func() ([]timeline.Event, error) {
		rows, err := tx.q.ListEvents(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Event) (timeline.Event, error) { return loadEvent(ctx, tx.q, r) })
	})
}

// UpdateEvent stores every field of e but its timeline, seq and created at, and replaces
// its tags with e.Tags, and adds attachments. When no source is called e.Source yet, it
// creates one with id newSourceID.
func (s *Store) UpdateEvent(ctx context.Context, e timeline.Event, newSourceID string, attachments ...NewAttachment) error {
	_, err := s.Change(ctx, ChangeOptions{Mode: Commit, Attach: attachments}, func(tx *Tx) error {
		err := tx.UpdateEvent(ctx, e, newSourceID)
		if err != nil {
			return err
		}
		tx.Attach(timeline.AttachmentOwner{EventID: new(e.ID)})
		return nil
	})
	return err
}

// UpdateEvent stores every field of e but its timeline, seq and created at, and replaces
// its tags with e.Tags. When no source is called e.Source yet, it creates one with id
// newSourceID.
func (tx *Tx) UpdateEvent(ctx context.Context, e timeline.Event, newSourceID string) error {
	err := e.Validate()
	if err != nil {
		return err
	}

	source, err := sourceID(ctx, tx.q, e.Source, newSourceID, e.UpdatedAt)
	if err != nil {
		return translate(err)
	}

	return translate(overwriteEvent(ctx, tx.q, e, source, nil))
}

// DeleteEvents deletes the events with ids, with their tags and annotations, or none of
// them when any does not exist. Their seqs are never given to other events.
func (s *Store) DeleteEvents(ctx context.Context, ids ...string) error {
	return s.change(ctx, Commit, nil, func(tx *Tx) error { return tx.DeleteEvents(ctx, ids...) })
}

// DeleteEvents deletes the events with ids, with their tags and annotations, or none of
// them when any does not exist. Their seqs are never given to other events.
func (tx *Tx) DeleteEvents(ctx context.Context, ids ...string) error {
	return translate(deleteAll(ctx, ids, tx.q.GetEvent, tx.q.DeleteEvent))
}

// deleteAll deletes the records with ids with del, after get has found every one of them.
func deleteAll[T any](
	ctx context.Context, ids []string, get func(context.Context, string) (T, error), del func(context.Context, string) error,
) error {
	for _, id := range ids {
		_, err := get(ctx, id)
		if err != nil {
			return err
		}
	}
	for _, id := range ids {
		err := del(ctx, id)
		if err != nil {
			return err
		}
	}

	return nil
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

// createEvent inserts e's row under seq, naming the source with sourceID, and its tags.
func createEvent(ctx context.Context, q *sqlcgen.Queries, e timeline.Event, seq int64, sourceID *string) error {
	_, err := q.CreateEvent(ctx, sqlcgen.CreateEventParams{
		ID:                e.ID,
		TimelineID:        e.TimelineID,
		Seq:               seq,
		OccurredAt:        timeline.FormatInstant(e.OccurredAt),
		OccurredOffset:    offsetParam(e.OccurredOffset),
		OccurredPrecision: string(e.Precision),
		Approximate:       sqliteBool(e.Approximate),
		OccurredRaw:       e.OccurredRaw,
		EndedAt:           formatOptional(e.EndedAt),
		SourceID:          sourceID,
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
}

// overwriteEvent stores e's fields but its timeline and seq, naming the source with sourceID
// and setting created at to createdAt unless it is nil, and replaces its tags.
func overwriteEvent(ctx context.Context, q *sqlcgen.Queries, e timeline.Event, sourceID, createdAt *string) error {
	_, err := q.OverwriteEvent(ctx, sqlcgen.OverwriteEventParams{
		OccurredAt:        timeline.FormatInstant(e.OccurredAt),
		OccurredOffset:    offsetParam(e.OccurredOffset),
		OccurredPrecision: string(e.Precision),
		Approximate:       sqliteBool(e.Approximate),
		OccurredRaw:       e.OccurredRaw,
		EndedAt:           formatOptional(e.EndedAt),
		SourceID:          sourceID,
		Text:              e.Text,
		Url:               e.URL,
		Author:            e.Author,
		CreatedAt:         createdAt,
		UpdatedAt:         timeline.FormatInstant(e.UpdatedAt),
		ID:                e.ID,
	})
	if err != nil {
		return err
	}

	return replaceTags(ctx, q, e.ID, e.Tags)
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

func replaceTags(ctx context.Context, q *sqlcgen.Queries, eventID string, tags []string) error {
	stored, err := q.ListEventTags(ctx, eventID)
	if err != nil {
		return err
	}
	for _, tag := range stored {
		err = q.DeleteEventTag(ctx, sqlcgen.DeleteEventTagParams{EventID: eventID, Tag: tag})
		if err != nil {
			return err
		}
	}

	return addTags(ctx, q, eventID, tags)
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
