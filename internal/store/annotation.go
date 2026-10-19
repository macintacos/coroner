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
	err := a.Validate()
	if err != nil {
		return err
	}
	_, err = s.writeWithAttachments(ctx, timeline.AttachmentOwner{AnnotationID: new(a.ID)}, attachments, func(q *sqlcgen.Queries) error {
		err := checkTarget(ctx, q, a.TimelineID, a.EventID)
		if err != nil {
			return err
		}
		source, err := sourceID(ctx, q, a.Source, newSourceID, a.CreatedAt)
		if err != nil {
			return err
		}

		return createAnnotation(ctx, q, a, source)
	})
	return err
}

// Annotation returns the annotation with id.
func (s *Store) Annotation(ctx context.Context, id string) (timeline.Annotation, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) (timeline.Annotation, error) {
		row, err := q.GetAnnotation(ctx, id)
		if err != nil {
			return timeline.Annotation{}, err
		}

		return loadAnnotation(ctx, q, row)
	})
}

// Annotations returns every annotation in the timeline with timelineID, on its events or
// on the timeline itself, oldest first.
func (s *Store) Annotations(ctx context.Context, timelineID string) ([]timeline.Annotation, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Annotation, error) {
		rows, err := q.ListAnnotations(ctx, timelineID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Annotation) (timeline.Annotation, error) { return loadAnnotation(ctx, q, r) })
	})
}

// EventAnnotations returns the annotations on the event with eventID, oldest first.
func (s *Store) EventAnnotations(ctx context.Context, eventID string) ([]timeline.Annotation, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Annotation, error) {
		rows, err := q.ListEventAnnotations(ctx, &eventID)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Annotation) (timeline.Annotation, error) { return loadAnnotation(ctx, q, r) })
	})
}

// UpdateAnnotation stores a's kind, body, author, source, URL and updated at, and adds
// attachments. When no source is called a.Source yet, it creates one with id newSourceID.
func (s *Store) UpdateAnnotation(ctx context.Context, a timeline.Annotation, newSourceID string, attachments ...NewAttachment) error {
	err := a.Validate()
	if err != nil {
		return err
	}
	_, err = s.writeWithAttachments(ctx, timeline.AttachmentOwner{AnnotationID: new(a.ID)}, attachments, func(q *sqlcgen.Queries) error {
		source, err := sourceID(ctx, q, a.Source, newSourceID, a.UpdatedAt)
		if err != nil {
			return err
		}

		_, err = q.UpdateAnnotation(ctx, sqlcgen.UpdateAnnotationParams{
			Kind:      string(a.Kind),
			Body:      a.Body,
			Author:    a.Author,
			SourceID:  source,
			Url:       a.URL,
			UpdatedAt: timeline.FormatInstant(a.UpdatedAt),
			ID:        a.ID,
		})
		return err
	})
	return err
}

// AnnotationsByIDPrefix returns up to limit annotations, in any timeline, whose id starts
// with prefix, ordered by id. prefix is matched with GLOB, so it must be non-empty lowercase
// hex and dashes: *, ? and [ are patterns, and ids are lowercase.
func (s *Store) AnnotationsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Annotation, error) {
	if prefix == "" || strings.Trim(prefix, "0123456789abcdef-") != "" {
		return nil, fmt.Errorf("annotation id prefix %q is not lowercase hex and dashes", prefix)
	}

	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Annotation, error) {
		rows, err := q.ListAnnotationsByIDPrefix(ctx, sqlcgen.ListAnnotationsByIDPrefixParams{ID: prefix + "*", Limit: int64(limit)})
		if err != nil {
			return nil, err
		}

		return loadAll(rows, func(r sqlcgen.Annotation) (timeline.Annotation, error) { return loadAnnotation(ctx, q, r) })
	})
}

// DeleteAnnotations deletes the annotations with ids, or none of them when any does not
// exist.
func (s *Store) DeleteAnnotations(ctx context.Context, ids ...string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		for _, id := range ids {
			_, err := q.GetAnnotation(ctx, id)
			if err != nil {
				return err
			}
		}
		for _, id := range ids {
			err := q.DeleteAnnotation(ctx, id)
			if err != nil {
				return err
			}
		}

		return nil
	})
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
