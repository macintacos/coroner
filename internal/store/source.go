package store

import (
	"context"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// Sources returns every source, ordered by name.
func (s *Store) Sources(ctx context.Context) ([]timeline.Source, error) {
	return read(ctx, s, func(q *sqlcgen.Queries) ([]timeline.Source, error) {
		rows, err := q.ListSources(ctx)
		if err != nil {
			return nil, err
		}

		return loadAll(rows, loadSource)
	})
}

func loadSource(row sqlcgen.Source) (timeline.Source, error) {
	var p instants
	source := timeline.Source{ID: row.ID, Name: row.Name, CreatedAt: p.parse(row.CreatedAt)}

	return source, p.err
}

// sourceID returns the id of the source called name, first creating it with newID and
// createdAt when no source has that name. A nil name gives a nil id.
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

// sourceName returns the name of the source with id. A nil id gives a nil name.
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
