package store

import (
	"context"
	"database/sql"
	"time"
)

// TagUsage is a tag with how many events carry it and when they happened.
type TagUsage struct {
	Name       string
	EventCount int
	// FirstOccurredAt and LastOccurredAt are never zero: a tag exists only on an event.
	FirstOccurredAt, LastOccurredAt time.Time
}

// tagUsageQuery returns TagUsages' SQL and args: every tag by name, or, given timelineIDs, only
// those timelines' tags, the one on the latest event first. Hand-written: sqlc cannot type MIN
// or MAX, nor bind a variable IN list.
func tagUsageQuery(timelineIDs []string) (string, []any) {
	const selectFrom = `SELECT t.tag, COUNT(*), MIN(e.occurred_at), MAX(e.occurred_at)
FROM event_tag t JOIN event e ON e.id = t.event_id
`
	if len(timelineIDs) == 0 {
		return selectFrom + `GROUP BY t.tag
ORDER BY t.tag`, nil
	}

	in, args := inList(timelineIDs)

	return selectFrom + `WHERE e.timeline_id ` + in + `
GROUP BY t.tag
ORDER BY MAX(e.occurred_at) DESC, t.tag`, args
}

// TagUsages is Tx.TagUsages in its own Read.
func (s *Store) TagUsages(ctx context.Context, timelineIDs ...string) ([]TagUsage, error) {
	return readIn(ctx, s, func(tx *Tx) ([]TagUsage, error) { return tx.TagUsages(ctx, timelineIDs...) })
}

// TagUsages returns every tag an event carries, with its usage, ordered by name. Given
// timelineIDs, it counts only those timelines' events, the tag on the latest event first.
func (tx *Tx) TagUsages(ctx context.Context, timelineIDs ...string) ([]TagUsage, error) {
	query, args := tagUsageQuery(timelineIDs)

	return translated(func() ([]TagUsage, error) {
		return queryAll(ctx, tx.db, query, args, scanTagUsage)
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
