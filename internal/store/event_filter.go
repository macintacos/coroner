package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// Order is the direction ListEvents sorts events in, by occurred at, then seq.
type Order int

// Orders. Both sort keys flip together, so either order is total.
const (
	OrderAsc Order = iota
	OrderDesc
)

// EventFilter selects a timeline's events. Its zero value selects every event, ascending.
type EventFilter struct {
	// Since keeps events that occurred at or after it; nil leaves the window open.
	Since *time.Time
	// Until keeps events that occurred before it; nil leaves the window open.
	Until *time.Time
	// Sources keeps events from any of these sources; empty keeps every event.
	Sources []string
	// Tags keeps events carrying all of these tags; empty keeps every event.
	Tags []string
	// Contains keeps events whose text contains it, ignoring case. Only ASCII letters fold,
	// as in SQLite's lower().
	Contains string
	Order    Order
	// Limit caps the events returned; 0 or less returns every match.
	Limit int
}

// EventPage is the events a filter selected, up to its limit.
type EventPage struct {
	Events []timeline.Event
	// Total counts every match, before the limit.
	Total int
	// Truncated reports that the limit left matches out.
	Truncated bool
}

// ListEvents returns the events of the timeline with timelineID that f keeps, with their
// tags and source, ordered by occurred at, then seq.
func (s *Store) ListEvents(ctx context.Context, timelineID string, f EventFilter) (EventPage, error) {
	query, args := buildEventQuery(timelineID, f)

	return readTx(ctx, s, func(tx *sql.Tx) (EventPage, error) {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return EventPage{}, err
		}
		// rows.Err reports what went wrong; Close only matters on an early return.
		defer func() { _ = rows.Close() }()

		page := EventPage{Events: []timeline.Event{}}
		for rows.Next() {
			e, err := scanEvent(rows, &page.Total)
			if err != nil {
				return EventPage{}, err
			}
			page.Events = append(page.Events, e)
		}
		err = rows.Err()
		if err != nil {
			return EventPage{}, err
		}

		page.Truncated = len(page.Events) < page.Total
		return page, nil
	})
}

// scanEvent reads a row of eventSelect into an event, and its match count into total.
func scanEvent(rows *sql.Rows, total *int) (timeline.Event, error) {
	var (
		row    sqlcgen.Event
		source *string
		tags   string
	)
	err := rows.Scan(
		&row.ID, &row.TimelineID, &row.Seq, &row.OccurredAt, &row.OccurredOffset,
		&row.OccurredPrecision, &row.Approximate, &row.OccurredRaw, &row.EndedAt, &row.SourceID, &row.Text,
		&row.Url, &row.Author, &row.CreatedAt, &row.UpdatedAt,
		&source, &tags, total,
	)
	if err != nil {
		return timeline.Event{}, err
	}

	tagList := []string{}
	if tags != "" {
		tagList = strings.Split(tags, ",")
	}

	return eventFromRow(row, source, tagList)
}

// eventSelect reads an event's row with its source name, its tags joined by commas (the tag
// grammar has none) and the count of rows the WHERE clause matches, before any LIMIT.
const eventSelect = `SELECT e.id, e.timeline_id, e.seq, e.occurred_at, e.occurred_offset,
	e.occurred_precision, e.approximate, e.occurred_raw, e.ended_at, e.source_id, e.text,
	e.url, e.author, e.created_at, e.updated_at,
	src.name,
	COALESCE((SELECT group_concat(t.tag, ',' ORDER BY t.tag) FROM event_tag t WHERE t.event_id = e.id), ''),
	COUNT(*) OVER ()
FROM event e LEFT JOIN source src ON src.id = e.source_id`

// buildEventQuery returns the SQL selecting the events of the timeline with timelineID that
// f keeps, and its args. Every value is an arg, never SQL text.
func buildEventQuery(timelineID string, f EventFilter) (string, []any) {
	where := []string{"e.timeline_id = ?"}
	args := []any{timelineID}

	if f.Since != nil {
		where = append(where, "e.occurred_at >= ?")
		args = append(args, timeline.FormatInstant(*f.Since))
	}
	if f.Until != nil {
		where = append(where, "e.occurred_at < ?")
		args = append(args, timeline.FormatInstant(*f.Until))
	}
	if len(f.Sources) > 0 {
		where = append(where, "src.name IN ("+strings.Repeat("?,", len(f.Sources)-1)+"?)")
		for _, source := range f.Sources {
			args = append(args, source)
		}
	}
	for _, tag := range f.Tags {
		where = append(where, "EXISTS (SELECT 1 FROM event_tag t WHERE t.event_id = e.id AND t.tag = ?)")
		args = append(args, tag)
	}
	if f.Contains != "" {
		// instr, not LIKE, so % and _ in the input match literally.
		where = append(where, "instr(lower(e.text), lower(?)) > 0")
		args = append(args, f.Contains)
	}

	direction := "ASC"
	if f.Order == OrderDesc {
		direction = "DESC"
	}
	query := eventSelect + " WHERE " + strings.Join(where, " AND ") +
		" ORDER BY e.occurred_at " + direction + ", e.seq " + direction

	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	return query, args
}
