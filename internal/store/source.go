package store

import (
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

// sourceUsageQuery is hand-written: sqlc cannot type a MIN or MAX that is null for a source
// with no events.
const sourceUsageQuery = `SELECT s.id, s.name, s.created_at,
	COALESCE(e.n, 0), COALESCE(a.n, 0), e.first, e.last
FROM source s
LEFT JOIN (SELECT source_id, COUNT(*) n, MIN(occurred_at) first, MAX(occurred_at) last
	FROM event WHERE source_id IS NOT NULL GROUP BY source_id) e ON e.source_id = s.id
LEFT JOIN (SELECT source_id, COUNT(*) n
	FROM annotation WHERE source_id IS NOT NULL GROUP BY source_id) a ON a.source_id = s.id
ORDER BY s.name`

// SourceUsages returns every source with its usage, ordered by name.
func (s *Store) SourceUsages(ctx context.Context) ([]SourceUsage, error) {
	return readTx(ctx, s, func(tx sqlcgen.DBTX) ([]SourceUsage, error) {
		return queryAll(ctx, tx, sourceUsageQuery, nil, scanSourceUsage)
	})
}

// scanSourceUsage reads a row of sourceUsageQuery; keep it in step with that column order.
func scanSourceUsage(rows *sql.Rows) (SourceUsage, error) {
	var (
		usage               SourceUsage
		id, name, createdAt string
		first, last         *string
	)
	err := rows.Scan(&id, &name, &createdAt, &usage.EventCount, &usage.AnnotationCount, &first, &last)
	if err != nil {
		return SourceUsage{}, err
	}

	var p instants
	usage.Source = timeline.Source{ID: id, Name: name, CreatedAt: p.parse(createdAt)}
	usage.FirstOccurredAt = p.parseOptional(first)
	usage.LastOccurredAt = p.parseOptional(last)

	return usage, p.err
}

// RenameSource sets the name of the source with id. Its events and annotations name it by id,
// so they follow.
func (s *Store) RenameSource(ctx context.Context, id, name string) error {
	err := timeline.ValidateSourceName(name)
	if err != nil {
		return err
	}

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
