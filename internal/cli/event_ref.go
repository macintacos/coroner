package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	// eventRefHelp is the part of an event command's help that says which references it takes.
	eventRefHelp = "An event is named by its seq in the current timeline (12), by a timeline and seq\n" +
		"(mongo-outage#12), or by its id or the start of it, in any timeline. Seqs and id starts\n" +
		"are for people; scripts pass full ids."
	// shownCandidates is how many events an ambiguous id prefix lists.
	shownCandidates = 10
)

// eventRef is an event reference as typed: a seq in timeline, or in the selected timeline
// when timeline is empty, or else an id prefix.
type eventRef struct {
	timeline string
	seq      int64
	idPrefix string
}

// parseEventRef reads ref as a seq (12), a timeline's seq (mongo-outage#12) or an id prefix,
// which it lowercases. A seq never starts with 0, and UUIDv7 text does until the year 2527, so
// a bare number is always a seq.
func parseEventRef(ref string) (eventRef, error) {
	if seq, ok := parseSeq(ref); ok {
		return eventRef{seq: seq}, nil
	}

	if name, seqText, ok := strings.Cut(ref, "#"); ok {
		seq, ok := parseSeq(seqText)
		if name == "" || !ok {
			return eventRef{}, badEventRefError(ref)
		}

		return eventRef{timeline: name, seq: seq}, nil
	}

	prefix := strings.ToLower(ref)
	if prefix == "" || !strings.ContainsRune("0123456789abcdef", rune(prefix[0])) ||
		strings.Trim(prefix, "0123456789abcdef-") != "" {
		return eventRef{}, badEventRefError(ref)
	}

	return eventRef{idPrefix: prefix}, nil
}

func badEventRefError(ref string) *Error {
	return &Error{Kind: KindUsage, Message: fmt.Sprintf("%q is not an event reference", ref), Hint: eventRefHelp}
}

// parseSeq returns s as a seq: digits with no leading zero.
func parseSeq(s string) (int64, bool) {
	if s == "" || s[0] == '0' || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}

	seq, err := strconv.ParseInt(s, 10, 64)
	return seq, err == nil
}

// timelineSelection is what selects the timeline a bare seq is in: -t, then CORONER_TIMELINE,
// then the current timeline.
type timelineSelection struct {
	flag      *string
	lookupEnv func(string) (string, bool)
}

// findEvent returns the event ref names and its timeline. A bare seq is in the selected
// timeline, and a <timeline>#<seq> in the timeline it names; an id prefix searches every
// timeline. -t applies to a bare seq only: with any other ref it is usage.
func findEvent(
	ctx context.Context, s *store.Store, ref eventRef, selection timelineSelection,
) (timeline.Event, timeline.Timeline, error) {
	if selection.flag != nil && (ref.idPrefix != "" || ref.timeline != "") {
		return timeline.Event{}, timeline.Timeline{}, &Error{
			Kind: KindUsage, Message: "-t selects the timeline of a bare seq only", Hint: eventRefHelp,
		}
	}
	if ref.idPrefix != "" {
		return findEventByID(ctx, s, ref.idPrefix)
	}

	t, err := seqTimeline(ctx, s, ref.timeline, selection)
	if err != nil {
		return timeline.Event{}, t, err
	}
	e, err := s.EventBySeq(ctx, t.ID, ref.seq)
	if errors.Is(err, store.ErrNotFound) {
		return e, t, eventNotFoundError(fmt.Sprintf("timeline `%s` has no event #%d", t.Name, ref.seq), err)
	}

	return e, t, err
}

// seqTimeline returns the timeline name names, by name or id, or the selected one when name is
// empty.
func seqTimeline(ctx context.Context, s *store.Store, name string, selection timelineSelection) (timeline.Timeline, error) {
	if name == "" {
		return selectTimeline(ctx, s, selection.flag, selection.lookupEnv)
	}

	return findTimeline(ctx, s, name)
}

// findEventByID returns the one event whose id starts with prefix, and its timeline.
func findEventByID(ctx context.Context, s *store.Store, prefix string) (timeline.Event, timeline.Timeline, error) {
	events, err := s.EventsByIDPrefix(ctx, prefix, shownCandidates+1)
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

func eventNotFoundError(message string, err error) *Error {
	return &Error{Kind: KindEventNotFound, Message: message, Hint: "Run 'coroner event list' to see a timeline's events.", Err: err}
}

// ambiguousEventError returns ambiguous_id, listing the first shownCandidates of events as
// <timeline>#<seq> and id.
func ambiguousEventError(ctx context.Context, s *store.Store, prefix string, events []timeline.Event) error {
	lines := []string{"It matches:"}
	for _, e := range events[:min(len(events), shownCandidates)] {
		t, err := s.Timeline(ctx, e.TimelineID)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("  %s#%d  %s", t.Name, e.Seq, e.ID))
	}
	if len(events) > shownCandidates {
		lines = append(lines, "  and more")
	}
	lines = append(lines, "Pass more of the id.")

	return &Error{
		Kind:    KindAmbiguousID,
		Message: "more than one event has an id starting " + prefix,
		Hint:    strings.Join(lines, "\n"),
	}
}
