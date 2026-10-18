package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

// Order is the direction ListEvents sorts events in, by occurred at, then seq.
type Order int

// OrderDesc is the exact reverse of OrderAsc: seq flips with occurred at. Any other value
// sorts ascending.
const (
	OrderAsc Order = iota
	OrderDesc
)

// EventFilter selects a timeline's events. Its zero value selects every event, ascending.
type EventFilter struct {
	// Since keeps events whose occurred at is at or after it, so an interval that started
	// earlier is left out even if it ends later; nil leaves the window open.
	Since *time.Time
	// Until keeps events whose occurred at is before it, even an interval that ends after it;
	// nil leaves the window open.
	Until *time.Time
	// Sources keeps events from any of these sources; empty keeps every event.
	Sources []string
	// Tags keeps events carrying all of these tags; empty keeps every event.
	Tags []string
	// Contains keeps events whose text contains it, ignoring case. Only ASCII letters fold,
	// as in SQLite's lower().
	Contains string
	// Order is OrderAsc unless set.
	Order Order
	// Limit caps the events returned; 0 or less returns every match.
	Limit int
}

// EventPage is the events a filter selected, up to its limit.
type EventPage struct {
	// Events is never nil, even when nothing matches.
	Events []timeline.Event
	// Total counts every match, before the limit.
	Total int
	// Truncated reports that the limit left matches out.
	Truncated bool
}

// ListEvents returns the events of the timeline with timelineID that f keeps, with their
// tags and source, ordered by occurred at, then seq. An unknown timeline gives an empty page,
// not ErrNotFound, and a bound outside years 0000–9999 is an error.
func (s *Store) ListEvents(ctx context.Context, timelineID string, f EventFilter) (EventPage, error) {
	err := errors.Join(checkBound("since", f.Since), checkBound("until", f.Until))
	if err != nil {
		return EventPage{}, err
	}
	query, args := buildEventQuery(timelineID, f)

	return readTx(ctx, s, func(tx sqlcgen.DBTX) (EventPage, error) {
		events, err := queryEvents(ctx, tx, query, args)
		if err != nil {
			return EventPage{}, err
		}

		page := EventPage{Events: events, Total: len(events)}
		if f.Limit > 0 && len(events) == f.Limit {
			countQuery, countArgs := buildEventCount(timelineID, f)
			err = tx.QueryRowContext(ctx, countQuery, countArgs...).Scan(&page.Total)
			if err != nil {
				return EventPage{}, err
			}
		}

		page.Truncated = len(page.Events) < page.Total
		return page, nil
	})
}

// checkBound rejects a bound whose year FormatInstant cannot write in four digits: it would
// sort out of place among stored instants.
func checkBound(what string, bound *time.Time) error {
	if bound == nil {
		return nil
	}
	if year := bound.UTC().Year(); year < 0 || year > 9999 {
		return fmt.Errorf("%s: year %d is outside 0000–9999", what, year)
	}

	return nil
}

func queryEvents(ctx context.Context, tx sqlcgen.DBTX, query string, args []any) ([]timeline.Event, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	// rows.Err reports what went wrong; Close only matters on an early return.
	defer func() { _ = rows.Close() }()

	events := []timeline.Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

// scanEvent reads a row of eventSelect. Scans e.* in sqlcgen.Event's field order; keep in
// step with the event table.
func scanEvent(rows *sql.Rows) (timeline.Event, error) {
	var (
		row      sqlcgen.Event
		source   *string
		tagsJSON string
	)
	err := rows.Scan(
		&row.ID, &row.TimelineID, &row.Seq, &row.OccurredAt, &row.OccurredOffset,
		&row.OccurredPrecision, &row.Approximate, &row.OccurredRaw, &row.EndedAt, &row.SourceID, &row.Text,
		&row.Url, &row.Author, &row.CreatedAt, &row.UpdatedAt,
		&source, &tagsJSON,
	)
	if err != nil {
		return timeline.Event{}, err
	}

	var tagList []string
	err = json.Unmarshal([]byte(tagsJSON), &tagList)
	if err != nil {
		return timeline.Event{}, err
	}

	return eventFromRow(row, source, tagList)
}

const eventFrom = `FROM event e LEFT JOIN source src ON src.id = e.source_id`

// eventSelect reads an event's columns, its source name and its tags as a sorted JSON array.
const eventSelect = `SELECT e.*, src.name,
	(SELECT json_group_array(t.tag ORDER BY t.tag) FROM event_tag t WHERE t.event_id = e.id)
` + eventFrom

// buildEventQuery returns the SQL selecting the events of the timeline with timelineID that
// f keeps, in its order and up to its limit, and its args.
func buildEventQuery(timelineID string, f EventFilter) (string, []any) {
	where, args := eventWhere(timelineID, f)

	direction := "ASC"
	if f.Order == OrderDesc {
		direction = "DESC"
	}
	query := eventSelect + " WHERE " + where + " ORDER BY e.occurred_at " + direction + ", e.seq " + direction

	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	return query, args
}

// buildEventCount returns the SQL counting every event buildEventQuery selects before its
// limit, and its args.
func buildEventCount(timelineID string, f EventFilter) (string, []any) {
	where, args := eventWhere(timelineID, f)
	return "SELECT COUNT(*) " + eventFrom + " WHERE " + where, args
}

// eventWhere returns the conditions selecting the events of the timeline with timelineID
// that f keeps, and their args. Every value is an arg, never SQL text.
func eventWhere(timelineID string, f EventFilter) (string, []any) {
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

	return strings.Join(where, " AND "), args
}
