package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
)

// TagUsage is a tag with how many events carry it and when they happened.
type TagUsage struct {
	Name       string
	EventCount int
	// FirstOccurredAt and LastOccurredAt are never zero: a tag exists only on an event.
	FirstOccurredAt, LastOccurredAt time.Time
}

// tagUsageQuery is hand-written: sqlc cannot type MIN or MAX.
const tagUsageQuery = `SELECT t.tag, COUNT(*), MIN(e.occurred_at), MAX(e.occurred_at)
FROM event_tag t JOIN event e ON e.id = t.event_id
GROUP BY t.tag
ORDER BY t.tag`

// TagUsages returns every tag an event carries, with its usage, ordered by name.
func (s *Store) TagUsages(ctx context.Context) ([]TagUsage, error) {
	return readTx(ctx, s, func(tx sqlcgen.DBTX) ([]TagUsage, error) {
		return queryAll(ctx, tx, tagUsageQuery, nil, scanTagUsage)
	})
}

// scanTagUsage reads a row of tagUsageQuery; keep it in step with that column order.
func scanTagUsage(rows *sql.Rows) (TagUsage, error) {
	var (
		usage       TagUsage
		first, last string
	)
	err := rows.Scan(&usage.Name, &usage.EventCount, &first, &last)
	if err != nil {
		return TagUsage{}, err
	}

	var p instants
	usage.FirstOccurredAt = p.parse(first)
	usage.LastOccurredAt = p.parse(last)

	return usage, p.err
}
