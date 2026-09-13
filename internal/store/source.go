package store

import (
	"cmp"
	"context"
	"database/sql"
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

// SourceUsage is a source with how many events and annotations name it, and when its events
// happened.
type SourceUsage struct {
	timeline.Source

	EventCount, AnnotationCount int
	// FirstOccurredAt and LastOccurredAt are nil when no event names the source.
	FirstOccurredAt, LastOccurredAt *time.Time
}

// sourceUsageQuery is hand-written because sqlc types MIN and MAX as never null, and a source
// with no events has neither.
const sourceUsageQuery = `SELECT s.id, s.name, s.created_at,
	(SELECT COUNT(*) FROM event e WHERE e.source_id = s.id),
	(SELECT COUNT(*) FROM annotation a WHERE a.source_id = s.id),
	(SELECT MIN(e.occurred_at) FROM event e WHERE e.source_id = s.id),
	(SELECT MAX(e.occurred_at) FROM event e WHERE e.source_id = s.id)
FROM source s
ORDER BY s.name`

// SourceUsages returns every source with its usage, ordered by name.
func (s *Store) SourceUsages(ctx context.Context) ([]SourceUsage, error) {
	return readTx(ctx, s, func(tx sqlcgen.DBTX) ([]SourceUsage, error) {
		return queryAll(ctx, tx, sourceUsageQuery, nil, scanSourceUsage)
	})
}

func scanSourceUsage(rows *sql.Rows) (SourceUsage, error) {
	var (
		row         sqlcgen.Source
		usage       SourceUsage
		first, last *string
	)
	err := rows.Scan(&row.ID, &row.Name, &row.CreatedAt, &usage.EventCount, &usage.AnnotationCount, &first, &last)
	if err != nil {
		return SourceUsage{}, err
	}

	var p instants
	usage.FirstOccurredAt = p.parseOptional(first)
	usage.LastOccurredAt = p.parseOptional(last)
	usage.Source, err = loadSource(row)

	return usage, cmp.Or(p.err, err)
}

// RenameSource sets the name of the source with id. Its events and annotations name it by id,
// so they follow.
func (s *Store) RenameSource(ctx context.Context, id, name string) error {
	return s.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.UpdateSource(ctx, sqlcgen.UpdateSourceParams{Name: name, ID: id})
		return err
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
