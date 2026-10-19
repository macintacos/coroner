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

// Order is the direction ListEvents sorts events in, by occurred at, then timeline id, then
// seq.
type Order int

// OrderDesc is the exact reverse of OrderAsc: timeline id and seq flip with occurred at. Any
// other value sorts ascending.
const (
	OrderAsc Order = iota
	OrderDesc
)

// EventFilter selects the events of one or more timelines. Given only TimelineIDs, it
// selects every event of those timelines, ascending.
type EventFilter struct {
	// TimelineIDs keeps events of any of these timelines, and must name at least one.
	TimelineIDs []string
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

// ListEvents returns the events of f's timelines that f keeps, with their tags and source,
// merged in timeline.CompareEvents order. An unknown timeline adds no events, not
// ErrNotFound; a filter naming no timeline, or a bound outside years 0000–9999, is an
// error.
func (s *Store) ListEvents(ctx context.Context, f EventFilter) (EventPage, error) {
	return readIn(ctx, s, func(tx *Tx) (EventPage, error) { return tx.ListEvents(ctx, f) })
}

// ListEvents returns the events of f's timelines that f keeps, with their tags and source,
// merged in timeline.CompareEvents order. An unknown timeline adds no events, not
// ErrNotFound; a filter naming no timeline, or a bound outside years 0000–9999, is an
// error.
func (tx *Tx) ListEvents(ctx context.Context, f EventFilter) (EventPage, error) {
	if len(f.TimelineIDs) == 0 {
		return EventPage{}, errors.New("store: EventFilter names no timeline")
	}
	err := errors.Join(checkBound("since", f.Since), checkBound("until", f.Until))
	if err != nil {
		return EventPage{}, err
	}
	query, args := buildEventQuery(f)

	return translated(func() (EventPage, error) {
		events, err := queryAll(ctx, tx.db, query, args, scanEvent)
		if err != nil {
			return EventPage{}, err
		}

		page := EventPage{Events: events, Total: len(events)}
		if f.Limit > 0 && len(events) == f.Limit {
			countQuery, countArgs := buildEventCount(f)
			err = tx.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&page.Total)
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

// buildEventQuery returns the SQL selecting the events f keeps, in its order and up to its
// limit, and its args. The order mirrors timeline.CompareEvents.
func buildEventQuery(f EventFilter) (string, []any) {
	where, args := eventWhere(f)

	direction := "ASC"
	if f.Order == OrderDesc {
		direction = "DESC"
	}
	query := eventSelect + " WHERE " + where +
		" ORDER BY e.occurred_at " + direction + ", e.timeline_id " + direction + ", e.seq " + direction

	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	return query, args
}

// buildEventCount returns the SQL counting every event buildEventQuery selects before its
// limit, and its args.
func buildEventCount(f EventFilter) (string, []any) {
	where, args := eventWhere(f)
	return "SELECT COUNT(*) " + eventFrom + " WHERE " + where, args
}

// eventWhere returns the conditions selecting the events f keeps, and their args. Every value
// is an arg, never SQL text.
func eventWhere(f EventFilter) (string, []any) {
	timelineIn, args := inList(f.TimelineIDs)
	where := []string{"e.timeline_id " + timelineIn}

	if f.Since != nil {
		where = append(where, "e.occurred_at >= ?")
		args = append(args, timeline.FormatInstant(*f.Since))
	}
	if f.Until != nil {
		where = append(where, "e.occurred_at < ?")
		args = append(args, timeline.FormatInstant(*f.Until))
	}
	if len(f.Sources) > 0 {
		sourceIn, sourceArgs := inList(f.Sources)
		where = append(where, "src.name "+sourceIn)
		args = append(args, sourceArgs...)
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

// inList returns an IN operator with a placeholder per value, and the values as its args.
func inList(values []string) (string, []any) {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}

	return "IN (" + strings.TrimSuffix(strings.Repeat("?,", len(values)), ",") + ")", args
}
