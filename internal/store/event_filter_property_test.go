package store_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// textRunes mixes case, a letter SQLite's lower() does not fold, and LIKE's wildcards.
const textRunes = "aAbBéÉ%_"

// Plain test, not a Ginkgo spec: rapid keys its repro hint and fail files on the test name.
func TestListEventsMatchesReferenceFilter(t *testing.T) {
	ctx := t.Context()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "coroner.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := s.Close()
		if err != nil {
			t.Error(err)
		}
	})

	// One store for every run, so each run gets a timeline and ids no earlier run used.
	n := 0
	nextID := func() string {
		n++
		return uuid(n)
	}

	rapid.Check(t, func(t *rapid.T) {
		created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
		tl := timeline.Timeline{ID: nextID(), Refs: []string{}, CreatedAt: created, UpdatedAt: created}
		tl.Name = "p" + tl.ID[len(tl.ID)-12:]
		err := s.CreateTimeline(ctx, tl)
		if err != nil {
			t.Fatal(err)
		}

		var added []timeline.Event
		for _, e := range rapid.SliceOfN(event(created), 0, 12).Draw(t, "events") {
			e.ID, e.TimelineID = nextID(), tl.ID
			e, err = s.AddEvent(ctx, e, nextID())
			if err != nil {
				t.Fatal(err)
			}
			added = append(added, e)
		}
		f := filter(len(added)).Draw(t, "filter")

		page, err := s.ListEvents(ctx, tl.ID, f)
		if err != nil {
			t.Fatal(err)
		}

		want, total := referenceFilter(added, f)
		if !slices.Equal(ids(page.Events), ids(want)) || page.Total != total || page.Truncated != (len(want) < total) {
			t.Fatalf("got %v, total %d, truncated %t; want %v, total %d, truncated %t",
				ids(page.Events), page.Total, page.Truncated, ids(want), total, len(want) < total)
		}
	})
}

// referenceFilter is ListEvents over events in memory: what f keeps, in order and up to its
// limit, and how many it keeps before the limit.
func referenceFilter(events []timeline.Event, f store.EventFilter) ([]timeline.Event, int) {
	kept := []timeline.Event{}
	for _, e := range events {
		if keeps(f, e) {
			kept = append(kept, e)
		}
	}
	slices.SortFunc(kept, timeline.CompareEvents)
	if f.Order == store.OrderDesc {
		slices.Reverse(kept)
	}

	total := len(kept)
	if f.Limit > 0 && f.Limit < total {
		kept = kept[:f.Limit]
	}

	return kept, total
}

func keeps(f store.EventFilter, e timeline.Event) bool {
	inWindow := (f.Since == nil || !e.OccurredAt.Before(*f.Since)) && (f.Until == nil || e.OccurredAt.Before(*f.Until))
	fromSource := len(f.Sources) == 0 || (e.Source != nil && slices.Contains(f.Sources, *e.Source))

	return inWindow && fromSource && hasEveryTag(e.Tags, f.Tags) && strings.Contains(asciiLower(e.Text), asciiLower(f.Contains))
}

func hasEveryTag(have, want []string) bool {
	for _, tag := range want {
		if !slices.Contains(have, tag) {
			return false
		}
	}
	return true
}

// asciiLower folds only A–Z, as SQLite's lower() does.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

func ids(events []timeline.Event) []string {
	out := []string{}
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

// event draws an event without ids, on a few minutes' grid so instants often tie.
func event(created time.Time) *rapid.Generator[timeline.Event] {
	return rapid.Custom(func(t *rapid.T) timeline.Event {
		e := timeline.Event{
			OccurredAt: minute(0, 5).Draw(t, "occurred"),
			Precision:  timeline.PrecisionMinute,
			Source:     rapid.Ptr(rapid.SampledFrom(sourceNames()), true).Draw(t, "source"),
			Tags:       rapid.SliceOfNDistinct(rapid.SampledFrom(tagNames()), 0, 3, rapid.ID).Draw(t, "tags"),
			Text:       rapid.StringOfN(rapid.RuneFrom([]rune(textRunes)), 1, 8, -1).Draw(t, "text"),
			CreatedAt:  created,
			UpdatedAt:  created,
		}
		if rapid.Bool().Draw(t, "interval") {
			e.EndedAt = new(e.OccurredAt.Add(time.Duration(rapid.IntRange(0, 30).Draw(t, "length")) * time.Minute))
		}

		return e
	})
}

// filter draws a filter whose bounds land on and beside event's grid.
func filter(eventCount int) *rapid.Generator[store.EventFilter] {
	return rapid.Custom(func(t *rapid.T) store.EventFilter {
		return store.EventFilter{
			Since:    rapid.Ptr(minute(-1, 6), true).Draw(t, "since"),
			Until:    rapid.Ptr(minute(-1, 6), true).Draw(t, "until"),
			Sources:  rapid.SliceOfNDistinct(rapid.SampledFrom(append(sourceNames(), "absent")), 0, 2, rapid.ID).Draw(t, "sources"),
			Tags:     rapid.SliceOfNDistinct(rapid.SampledFrom(tagNames()), 0, 2, rapid.ID).Draw(t, "tags"),
			Contains: rapid.StringOfN(rapid.RuneFrom([]rune(textRunes)), 0, 3, -1).Draw(t, "contains"),
			Order:    rapid.SampledFrom([]store.Order{store.OrderAsc, store.OrderDesc}).Draw(t, "order"),
			Limit:    rapid.IntRange(0, eventCount+1).Draw(t, "limit"),
		}
	})
}

// minute draws an instant first to last minutes from 14:00 on one day.
func minute(first, last int) *rapid.Generator[time.Time] {
	return rapid.Custom(func(t *rapid.T) time.Time {
		m := rapid.IntRange(first, last).Draw(t, "minute")
		return time.Date(2026, 9, 12, 14, m, 0, 0, time.UTC)
	})
}

func sourceNames() []string { return []string{"pagerduty", "slack", "grafana"} }

func tagNames() []string { return []string{"phase:detect", "team:db", "sev1", "db-2"} }
