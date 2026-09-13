package store

import (
	"context"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// AddAnnotation adds a. When no source is called a.Source yet, it creates one with id
// newSourceID.
func (s *Store) AddAnnotation(ctx context.Context, a timeline.Annotation, newSourceID string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		source, err := sourceID(ctx, q, a.Source, newSourceID, a.CreatedAt)
		if err != nil {
			return err
		}

		_, err = q.CreateAnnotation(ctx, sqlcgen.CreateAnnotationParams{
			ID:         a.ID,
			TimelineID: a.TimelineID,
			EventID:    a.EventID,
			Kind:       string(a.Kind),
			Body:       a.Body,
			Author:     a.Author,
			SourceID:   source,
			Url:        a.URL,
			CreatedAt:  timeline.FormatInstant(a.CreatedAt),
			UpdatedAt:  timeline.FormatInstant(a.UpdatedAt),
		})
		return err
	})
}

// Annotation returns the annotation with id.
func (s *Store) Annotation(ctx context.Context, id string) (timeline.Annotation, error) {
	var a timeline.Annotation
	err := s.read(ctx, func(q *sqlcgen.Queries) error {
		row, err := q.GetAnnotation(ctx, id)
		if err != nil {
			return err
		}

		a, err = loadAnnotation(ctx, q, row)
		return err
	})

	return a, err
}

// Annotations returns every annotation in the timeline with timelineID, on its events or
// on the timeline itself, oldest first.
func (s *Store) Annotations(ctx context.Context, timelineID string) ([]timeline.Annotation, error) {
	return s.readAnnotations(ctx, func(q *sqlcgen.Queries) ([]sqlcgen.Annotation, error) {
		return q.ListAnnotations(ctx, timelineID)
	})
}

// EventAnnotations returns the annotations on the event with eventID, oldest first.
func (s *Store) EventAnnotations(ctx context.Context, eventID string) ([]timeline.Annotation, error) {
	return s.readAnnotations(ctx, func(q *sqlcgen.Queries) ([]sqlcgen.Annotation, error) {
		return q.ListEventAnnotations(ctx, &eventID)
	})
}

// UpdateAnnotation stores a's kind, body, author, source, URL and updated at. When no
// source is called a.Source yet, it creates one with id newSourceID.
func (s *Store) UpdateAnnotation(ctx context.Context, a timeline.Annotation, newSourceID string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
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
}

// DeleteAnnotation deletes the annotation with id.
func (s *Store) DeleteAnnotation(ctx context.Context, id string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.GetAnnotation(ctx, id)
		if err != nil {
			return err
		}

		return q.DeleteAnnotation(ctx, id)
	})
}

func (s *Store) readAnnotations(
	ctx context.Context, list func(*sqlcgen.Queries) ([]sqlcgen.Annotation, error),
) ([]timeline.Annotation, error) {
	var annotations []timeline.Annotation
	err := s.read(ctx, func(q *sqlcgen.Queries) error {
		rows, err := list(q)
		if err != nil {
			return err
		}

		annotations = make([]timeline.Annotation, 0, len(rows))
		for _, row := range rows {
			a, err := loadAnnotation(ctx, q, row)
			if err != nil {
				return err
			}
			annotations = append(annotations, a)
		}

		return nil
	})

	return annotations, err
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
