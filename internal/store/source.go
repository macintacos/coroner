package store

import (
	"context"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// Sources returns every source, ordered by name.
func (s *Store) Sources(ctx context.Context) ([]timeline.Source, error) {
	var sources []timeline.Source
	err := s.read(ctx, func(q *sqlcgen.Queries) error {
		rows, err := q.ListSources(ctx)
		if err != nil {
			return err
		}

		sources = make([]timeline.Source, 0, len(rows))
		for _, row := range rows {
			var p instants
			sources = append(sources, timeline.Source{ID: row.ID, Name: row.Name, CreatedAt: p.parse(row.CreatedAt)})
			if p.err != nil {
				return p.err
			}
		}

		return nil
	})

	return sources, err
}

// sourceID returns the id of the source called name, first creating it with newID and
// createdAt when no source has that name. A record without a source has neither.
func sourceID(
	ctx context.Context, q *sqlcgen.Queries, name *string, newID string, createdAt time.Time,
) (*string, error) {
	if name == nil {
		return name, nil
	}

	source, err := q.UpsertSource(ctx, sqlcgen.UpsertSourceParams{
		ID:        newID,
		Name:      *name,
		CreatedAt: timeline.FormatInstant(createdAt),
	})
	if err != nil {
		return nil, err
	}

	return &source.ID, nil
}

// sourceName returns the name of the source with id. A record without a source has
// neither.
func sourceName(ctx context.Context, q *sqlcgen.Queries, id *string) (*string, error) {
	if id == nil {
		return id, nil
	}

	source, err := q.GetSource(ctx, *id)
	if err != nil {
		return nil, err
	}

	return &source.Name, nil
}
