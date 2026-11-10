package clikit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	// EventRefHelp is the part of an event command's help that says which references it takes.
	EventRefHelp = "An event is named by its seq in the current timeline (12), by a timeline and seq\n" +
		"(mongo-outage#12), or by its id or the start of it, in any timeline. Seqs and id starts\n" +
		"are for people; scripts pass full ids."
	// ShownMatches is how many records an ambiguous id prefix lists.
	ShownMatches = 10
)

// EventRef is an event reference as typed: a seq in timeline, or in the selected timeline
// when timeline is empty, or else an id prefix.
type EventRef struct {
	timeline string
	seq      int64
	idPrefix string
}

// ParseEventRef reads ref as a seq (12), a timeline's seq (mongo-outage#12) or an id prefix,
// which it lowercases. A seq never starts with 0, and UUIDv7 text does until the year 2527, so
// a bare number is always a seq.
func ParseEventRef(ref string) (EventRef, error) {
	if seq, ok := parseSeq(ref); ok {
		return EventRef{seq: seq}, nil
	}

	if name, seqText, ok := strings.Cut(ref, "#"); ok {
		seq, ok := parseSeq(seqText)
		if name == "" || !ok {
			return EventRef{}, badEventRefError(ref)
		}

		return EventRef{timeline: name, seq: seq}, nil
	}

	prefix, ok := ParseIDPrefix(ref)
	if !ok {
		return EventRef{}, badEventRefError(ref)
	}

	return EventRef{idPrefix: prefix}, nil
}

// ParseIDPrefix returns ref lowercased, and whether that is an id prefix: hex digits and dashes,
// starting with a hex digit.
func ParseIDPrefix(ref string) (string, bool) {
	prefix := strings.ToLower(ref)
	return prefix, prefix != "" && strings.ContainsRune("0123456789abcdef", rune(prefix[0])) &&
		strings.Trim(prefix, "0123456789abcdef-") == ""
}

// CheckEventRef rejects a ref that ParseEventRef cannot read.
func CheckEventRef(ref string) error {
	_, err := ParseEventRef(ref)
	return err
}

func badEventRefError(ref string) *Error {
	return &Error{Kind: KindUsage, Message: fmt.Sprintf("%q is not an event reference", ref), Hint: EventRefHelp}
}

// parseSeq returns s as a seq: digits with no leading zero.
func parseSeq(s string) (int64, bool) {
	if s == "" || s[0] == '0' || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}

	seq, err := strconv.ParseInt(s, 10, 64)
	return seq, err == nil
}

// FindEvent returns the event ref names and its timeline. A bare seq is in the timeline -t,
// CORONER_TIMELINE or the current timeline selects, and a <timeline>#<seq> in the timeline it
// names; an id prefix searches every timeline. -t applies to a bare seq only: with any other
// ref it is usage.
func FindEvent(
	ctx context.Context, logger *slog.Logger, s Records, ref EventRef, timelines TimelineInput, lookupEnv func(string) (string, bool),
) (timeline.Event, timeline.Timeline, error) {
	if ref.idPrefix != "" || ref.timeline != "" {
		if len(timelines.Names) > 0 {
			return timeline.Event{}, timeline.Timeline{}, &Error{
				Kind: KindUsage, Message: "-t selects the timeline of a bare seq only", Hint: EventRefHelp,
			}
		}

		return findNamedEvent(ctx, s, ref)
	}

	t, err := SelectOne(ctx, logger, s, timelines, lookupEnv)
	if err != nil {
		return timeline.Event{}, t, err
	}
	e, err := eventBySeq(ctx, s, t, ref.seq)

	return e, t, err
}

// FindEventIn returns the event ref names, which must be in t: an event in another timeline
// is event_not_found.
func FindEventIn(ctx context.Context, s Records, t timeline.Timeline, ref EventRef) (timeline.Event, error) {
	if ref.idPrefix == "" && ref.timeline == "" {
		return eventBySeq(ctx, s, t, ref.seq)
	}
	e, eventTimeline, err := findNamedEvent(ctx, s, ref)
	if err != nil || e.TimelineID == t.ID {
		return e, err
	}

	return timeline.Event{}, &Error{
		Kind:    KindEventNotFound,
		Message: fmt.Sprintf("event %s#%d is not in timeline `%s`", eventTimeline.Name, e.Seq, t.Name),
		Hint:    "Pass -t " + eventTimeline.Name + " to act on its own timeline.",
	}
}

// findNamedEvent returns the event an id prefix or a <timeline>#<seq> ref names, and its
// timeline.
func findNamedEvent(ctx context.Context, s Records, ref EventRef) (timeline.Event, timeline.Timeline, error) {
	if ref.idPrefix != "" {
		return findEventByID(ctx, s, ref.idPrefix)
	}

	t, err := FindTimeline(ctx, s, ref.timeline)
	if err != nil {
		return timeline.Event{}, t, err
	}
	e, err := eventBySeq(ctx, s, t, ref.seq)

	return e, t, err
}

// eventBySeq returns t's event #seq, or event_not_found when it has none.
func eventBySeq(ctx context.Context, s Records, t timeline.Timeline, seq int64) (timeline.Event, error) {
	e, err := s.EventBySeq(ctx, t.ID, seq)
	if errors.Is(err, store.ErrNotFound) {
		return e, eventNotFoundError(fmt.Sprintf("timeline `%s` has no event #%d", t.Name, seq), err)
	}

	return e, err
}

// findEventByID returns the one event whose id starts with prefix, and its timeline.
func findEventByID(ctx context.Context, s Records, prefix string) (timeline.Event, timeline.Timeline, error) {
	events, err := s.EventsByIDPrefix(ctx, prefix, ShownMatches+1)
	switch {
	case err != nil:
		return timeline.Event{}, timeline.Timeline{}, err
	case len(events) == 0:
		return timeline.Event{}, timeline.Timeline{}, eventNotFoundError("no event has an id starting "+prefix, nil)
	case len(events) > 1:
		return timeline.Event{}, timeline.Timeline{}, ambiguousEventError(ctx, s, prefix, events)
	}

	t, err := s.Timeline(ctx, events[0].TimelineID)
	return events[0], t, err
}

// SeqRef names e as its timeline and seq, as in mongo-outage#12.
func SeqRef(t timeline.Timeline, e timeline.Event) string {
	return fmt.Sprintf("%s#%d", t.Name, e.Seq)
}

func eventNotFoundError(message string, err error) *Error {
	return &Error{Kind: KindEventNotFound, Message: message, Hint: "Run 'coroner event list' to see a timeline's events.", Err: err}
}

// ambiguousEventError returns ambiguous_id, listing the first ShownMatches of events as
// <timeline>#<seq> and id.
func ambiguousEventError(ctx context.Context, s Records, prefix string, events []timeline.Event) error {
	var rows []string
	for _, e := range events[:min(len(events), ShownMatches)] {
		t, err := s.Timeline(ctx, e.TimelineID)
		if err != nil {
			return err
		}
		rows = append(rows, fmt.Sprintf("%s#%d  %s", t.Name, e.Seq, e.ID))
	}

	return AmbiguousIDError("event", prefix, rows, len(events) > ShownMatches)
}

// AmbiguousIDError returns ambiguous_id for the noun records whose ids start with prefix,
// listing rows, and saying more matched when more did.
func AmbiguousIDError(noun, prefix string, rows []string, more bool) *Error {
	lines := []string{"It matches:"}
	for _, row := range rows {
		lines = append(lines, "  "+row)
	}
	if more {
		lines = append(lines, "  and more")
	}
	lines = append(lines, "Pass more of the id.")

	return &Error{
		Kind:    KindAmbiguousID,
		Message: "more than one " + noun + " has an id starting " + prefix,
		Hint:    strings.Join(lines, "\n"),
	}
}

// EventLinkIDs returns the ids of the links from or to any of eventIDs, each once, in the order
// first read. Unlike tx.TimelineLinks, it finds a link into the events' timeline from another.
func EventLinkIDs(ctx context.Context, tx *store.Tx, eventIDs []string) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, id := range eventIDs {
		links, err := tx.EventLinks(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, l := range links {
			if !seen[l.ID] {
				seen[l.ID] = true
				ids = append(ids, l.ID)
			}
		}
	}

	return ids, nil
}

// EventFlag returns the event reference -e gives, or nil when -e is not given.
func EventFlag(flags *pflag.FlagSet) (*EventRef, error) {
	return OptionalEventRef(ChangedString(flags, "event"))
}

// OptionalEventRef returns the event reference value gives, or nil when value is nil.
func OptionalEventRef(value *string) (*EventRef, error) {
	var ref *EventRef
	if value != nil {
		parsed, err := ParseEventRef(*value)
		if err != nil {
			return nil, err
		}
		ref = &parsed
	}

	return ref, nil
}

// eventPlace is where an event sits in its timeline: its position in time order, and its seq.
type eventPlace struct {
	Position int
	Seq      int64
}

// EventOrder is each event's place, keyed by event id.
type EventOrder map[string]eventPlace

// OrderEvents indexes events, which must be in timeline.CompareEvents order.
func OrderEvents(events []timeline.Event) EventOrder {
	order := make(EventOrder, len(events))
	for i, e := range events {
		order[e.ID] = eventPlace{Position: i, Seq: e.Seq}
	}

	return order
}

// FindEach returns the record find gives for each id prefix, in order and once each by id.
func FindEach[T any](prefixes []string, id func(T) string, find func(prefix string) (T, error)) ([]T, error) {
	found := make([]T, 0, len(prefixes))
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		record, err := find(prefix)
		if err != nil {
			return nil, err
		}
		if !seen[id(record)] {
			seen[id(record)] = true
			found = append(found, record)
		}
	}

	return found, nil
}
