package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// AddAnnotation adds a with attachments. When no source is called a.Source yet, it creates
// one with id newSourceID.
func (s *Store) AddAnnotation(ctx context.Context, a timeline.Annotation, newSourceID string, attachments ...NewAttachment) error {
	_, err := s.writeWithAttachments(ctx, timeline.AttachmentOwner{AnnotationID: new(a.ID)}, attachments, func(tx *Tx) error {
		return tx.AddAnnotation(ctx, a, newSourceID)
	})
	return err
}

// AddAnnotation adds a. When no source is called a.Source yet, it creates one with id
// newSourceID.
func (tx *Tx) AddAnnotation(ctx context.Context, a timeline.Annotation, newSourceID string) error {
	err := a.Validate()
	if err != nil {
		return err
	}

	return translate(addAnnotation(ctx, tx.q, a, newSourceID))
}

func addAnnotation(ctx context.Context, q *sqlcgen.Queries, a timeline.Annotation, newSourceID string) error {
	err := checkTarget(ctx, q, a.TimelineID, a.EventID)
	if err != nil {
		return err
	}
	source, err := sourceID(ctx, q, a.Source, newSourceID, a.CreatedAt)
	if err != nil {
		return err
	}

	return createAnnotation(ctx, q, a, source)
}

// Annotation returns the annotation with id.
func (s *Store) Annotation(ctx context.Context, id string) (timeline.Annotation, error) {
	return readIn(ctx, s, func(tx *Tx) (timeline.Annotation, error) { return tx.Annotation(ctx, id) })
}

// Annotation returns the annotation with id.
func (tx *Tx) Annotation(ctx context.Context, id string) (timeline.Annotation, error) {
	return translated(func() (timeline.Annotation, error) {
		row, err := tx.q.GetAnnotation(ctx, id)
		if err != nil {
			return timeline.Annotation{}, err
		}

		return loadAnnotation(ctx, tx.q, row)
	})
}

// Annotations returns every annotation in the timeline with timelineID, on its events or
// on the timeline itself, oldest first.
func (s *Store) Annotations(ctx context.Context, timelineID string) ([]timeline.Annotation, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Annotation, error) { return tx.Annotations(ctx, timelineID) })
}

// Annotations returns every annotation in the timeline with timelineID, on its events or
// on the timeline itself, oldest first.
func (tx *Tx) Annotations(ctx context.Context, timelineID string) ([]timeline.Annotation, error) {
	return translated(func() ([]timeline.Annotation, error) {
		rows, err := tx.q.ListAnnotations(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Annotation) (timeline.Annotation, error) { return loadAnnotation(ctx, tx.q, r) })
	})
}

// EventAnnotations returns the annotations on the event with eventID, oldest first.
func (s *Store) EventAnnotations(ctx context.Context, eventID string) ([]timeline.Annotation, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Annotation, error) { return tx.EventAnnotations(ctx, eventID) })
}

// EventAnnotations returns the annotations on the event with eventID, oldest first.
func (tx *Tx) EventAnnotations(ctx context.Context, eventID string) ([]timeline.Annotation, error) {
	return translated(func() ([]timeline.Annotation, error) {
		rows, err := tx.q.ListEventAnnotations(ctx, &eventID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Annotation) (timeline.Annotation, error) { return loadAnnotation(ctx, tx.q, r) })
	})
}

// UpdateAnnotation stores a's kind, body, author, source, URL and updated at, and adds
// attachments. When no source is called a.Source yet, it creates one with id newSourceID.
func (s *Store) UpdateAnnotation(ctx context.Context, a timeline.Annotation, newSourceID string, attachments ...NewAttachment) error {
	_, err := s.writeWithAttachments(ctx, timeline.AttachmentOwner{AnnotationID: new(a.ID)}, attachments, func(tx *Tx) error {
		return tx.UpdateAnnotation(ctx, a, newSourceID)
	})
	return err
}

// UpdateAnnotation stores a's kind, body, author, source, URL and updated at. When no source
// is called a.Source yet, it creates one with id newSourceID.
func (tx *Tx) UpdateAnnotation(ctx context.Context, a timeline.Annotation, newSourceID string) error {
	err := a.Validate()
	if err != nil {
		return err
	}

	source, err := sourceID(ctx, tx.q, a.Source, newSourceID, a.UpdatedAt)
	if err != nil {
		return translate(err)
	}

	return translate(overwriteAnnotation(ctx, tx.q, a, source, nil))
}

// overwriteAnnotation stores a's kind, body, author, URL and updated at, naming the source
// with sourceID and keeping the stored created at unless createdAt is set.
func overwriteAnnotation(ctx context.Context, q *sqlcgen.Queries, a timeline.Annotation, sourceID, createdAt *string) error {
	_, err := q.OverwriteAnnotation(ctx, sqlcgen.OverwriteAnnotationParams{
		Kind:      string(a.Kind),
		Body:      a.Body,
		Author:    a.Author,
		SourceID:  sourceID,
		Url:       a.URL,
		CreatedAt: createdAt,
		UpdatedAt: timeline.FormatInstant(a.UpdatedAt),
		ID:        a.ID,
	})
	return err
}

// AnnotationsByIDPrefix returns up to limit annotations, in any timeline, whose id starts
// with prefix, ordered by id. prefix is matched with GLOB, so it must be non-empty lowercase
// hex and dashes: *, ? and [ are patterns, and ids are lowercase.
func (s *Store) AnnotationsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Annotation, error) {
	return readIn(ctx, s, func(tx *Tx) ([]timeline.Annotation, error) { return tx.AnnotationsByIDPrefix(ctx, prefix, limit) })
}

// AnnotationsByIDPrefix returns up to limit annotations, in any timeline, whose id starts
// with prefix, ordered by id. prefix is matched with GLOB, so it must be non-empty lowercase
// hex and dashes: *, ? and [ are patterns, and ids are lowercase.
func (tx *Tx) AnnotationsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Annotation, error) {
	if prefix == "" || strings.Trim(prefix, "0123456789abcdef-") != "" {
		return nil, fmt.Errorf("annotation id prefix %q is not lowercase hex and dashes", prefix)
	}

	return translated(func() ([]timeline.Annotation, error) {
		rows, err := tx.q.ListAnnotationsByIDPrefix(ctx, sqlcgen.ListAnnotationsByIDPrefixParams{ID: prefix + "*", Limit: int64(limit)})
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Annotation) (timeline.Annotation, error) { return loadAnnotation(ctx, tx.q, r) })
	})
}

// DeleteAnnotations deletes the annotations with ids, or none of them when any does not
// exist.
func (s *Store) DeleteAnnotations(ctx context.Context, ids ...string) error {
	return s.change(ctx, nil, func(tx *Tx) error { return tx.DeleteAnnotations(ctx, ids...) })
}

// DeleteAnnotations deletes the annotations with ids, or none of them when any does not
// exist.
func (tx *Tx) DeleteAnnotations(ctx context.Context, ids ...string) error {
	return translate(deleteAll(ctx, ids, tx.q.GetAnnotation, tx.q.DeleteAnnotation))
}

// checkTarget returns an error wrapping ErrNotFound unless the timeline with timelineID
// exists and eventID, when set, names an event in it.
func checkTarget(ctx context.Context, q *sqlcgen.Queries, timelineID string, eventID *string) error {
	_, err := q.GetTimeline(ctx, timelineID)
	if err != nil || eventID == nil {
		return err
	}

	event, err := q.GetEvent(ctx, *eventID)
	if err != nil {
		return err
	}
	if event.TimelineID != timelineID {
		return fmt.Errorf("event %s is not in timeline %s: %w", *eventID, timelineID, ErrNotFound)
	}

	return nil
}

// createAnnotation inserts a's row, naming the source with sourceID.
func createAnnotation(ctx context.Context, q *sqlcgen.Queries, a timeline.Annotation, sourceID *string) error {
	_, err := q.CreateAnnotation(ctx, sqlcgen.CreateAnnotationParams{
		ID:         a.ID,
		TimelineID: a.TimelineID,
		EventID:    a.EventID,
		Kind:       string(a.Kind),
		Body:       a.Body,
		Author:     a.Author,
		SourceID:   sourceID,
		Url:        a.URL,
		CreatedAt:  timeline.FormatInstant(a.CreatedAt),
		UpdatedAt:  timeline.FormatInstant(a.UpdatedAt),
	})
	return err
}

func loadAnnotation(ctx context.Context, q *sqlcgen.Queries, row sqlcgen.Annotation) (timeline.Annotation, error) {
	source, err := sourceName(ctx, q, row.SourceID)
	if err != nil {
		return timeline.Annotation{}, err
	}

	var p instants
	a := timeline.Annotation{
		ID:         row.ID,
		TimelineID: row.TimelineID,
		EventID:    row.EventID,
		Kind:       timeline.AnnotationKind(row.Kind),
		Body:       row.Body,
		Author:     row.Author,
		Source:     source,
		URL:        row.Url,
		CreatedAt:  p.parse(row.CreatedAt),
		UpdatedAt:  p.parse(row.UpdatedAt),
	}

	return a, p.err
}
