package store_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// Plain test, not a Ginkgo spec: rapid keys its repro hint and fail files on the test name.
func TestEventWritesReadBack(t *testing.T) {
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
		tl.Name = "w" + tl.ID[len(tl.ID)-12:]
		err := s.CreateTimeline(ctx, tl)
		if err != nil {
			t.Fatal(err)
		}
		id := nextID()
		readBack := func(step string, want timeline.Event) {
			got, err := s.Event(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("%s (-want +got):\n%s", step, diff)
			}
		}

		added := fullEvent(sourceNames()).Draw(t, "added")
		added.ID, added.TimelineID = id, tl.ID
		added, err = s.AddEvent(ctx, added, nextID())
		if err != nil {
			t.Fatal(err)
		}
		readBack("add", sortedTags(added))

		updated := fullEvent(sourceNames()).Draw(t, "updated")
		updated.ID, updated.TimelineID, updated.Seq = id, tl.ID, added.Seq
		err = s.UpdateEvent(ctx, updated, nextID())
		if err != nil {
			t.Fatal(err)
		}
		updated.CreatedAt = added.CreatedAt
		readBack("update", sortedTags(updated))

		var storedSources []string
		for _, source := range []*string{added.Source, updated.Source} {
			if source != nil {
				storedSources = append(storedSources, *source)
			}
		}
		replaced := fullEvent(storedSources).Draw(t, "replaced")
		replaced.ID, replaced.TimelineID, replaced.Seq = id, tl.ID, added.Seq
		err = s.Import(ctx, store.ImportRecords{EventReplacements: []timeline.Event{replaced}})
		if err != nil {
			t.Fatal(err)
		}
		readBack("replace", sortedTags(replaced))
	})
}

// sortedTags is e as the store reads it back: tags sorted, never nil.
func sortedTags(e timeline.Event) timeline.Event {
	e.Tags = append([]string{}, e.Tags...)
	slices.Sort(e.Tags)

	return e
}

// fullEvent draws an event without ids or seq, every optional field sometimes set, its occurred
// at the start of its precision window in its offset. It and the generators below copy event,
// optionalSource, tag, text, blankable and instant in internal/cli/roundtrip_test.go; change
// both together.
func fullEvent(sources []string) *rapid.Generator[timeline.Event] {
	return rapid.Custom(func(t *rapid.T) timeline.Event {
		e := timeline.Event{
			Precision: rapid.SampledFrom([]timeline.Precision{
				timeline.PrecisionDay, timeline.PrecisionHour, timeline.PrecisionMinute, timeline.PrecisionSecond,
				timeline.PrecisionMillisecond, timeline.PrecisionMicrosecond, timeline.PrecisionNanosecond,
			}).Draw(t, "precision"),
			OccurredOffset: rapid.Ptr(rapid.IntRange(-timeline.MaxOccurredOffset, timeline.MaxOccurredOffset), true).Draw(t, "offset"),
			Approximate:    rapid.Bool().Draw(t, "approximate"),
			OccurredRaw:    rapid.Ptr(anyBlankable(), true).Draw(t, "raw"),
			Source:         anySource(sources).Draw(t, "source"),
			Tags:           rapid.SliceOfNDistinct(anyTag(), 0, 4, rapid.ID).Draw(t, "tags"),
			Text:           anyText().Draw(t, "text"),
			URL:            rapid.Ptr(anyBlankable(), true).Draw(t, "url"),
			Author:         rapid.Ptr(anyBlankable(), true).Draw(t, "author"),
			CreatedAt:      anyInstant().Draw(t, "created"),
			UpdatedAt:      anyInstant().Draw(t, "updated"),
		}
		zone := time.UTC
		if e.OccurredOffset != nil {
			zone = time.FixedZone("", *e.OccurredOffset)
		}
		e.OccurredAt = e.Precision.Truncate(anyInstant().Draw(t, "occurred").In(zone)).UTC()
		if rapid.Bool().Draw(t, "interval") {
			e.EndedAt = new(e.OccurredAt.Add(time.Duration(rapid.Int64Range(0, int64(48*time.Hour)).Draw(t, "length"))))
		}

		return e
	})
}

func anySource(sources []string) *rapid.Generator[*string] {
	if len(sources) == 0 {
		return rapid.Just[*string](nil)
	}

	return rapid.Ptr(rapid.SampledFrom(sources), true)
}

func anyTag() *rapid.Generator[string] {
	return rapid.StringMatching(`^[a-z0-9][a-z0-9._:/-]{0,63}$`)
}

// anyText draws text the store's not-blank CHECK accepts: any Unicode, text that breaks HTML,
// lines or JSON, or a long run.
func anyText() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.StringN(1, 32, -1),
		rapid.SampledFrom([]string{"</script><script>alert(1)</script>", "line one\nline two\r\n", "Réplica caída — 東京 ✓", "a\u2028b\u0000c"}),
		rapid.StringN(1000, 5000, -1),
	).Filter(func(s string) bool {
		// SQLite's length() stops at a NUL, so the CHECK reads text whose trimmed start is NUL as blank.
		trimmed := strings.TrimSpace(s)
		return trimmed != "" && trimmed[0] != 0
	})
}

// anyBlankable draws text for a field with no not-blank rule, often empty or only whitespace.
func anyBlankable() *rapid.Generator[string] {
	return rapid.OneOf(rapid.SampledFrom([]string{"", " ", "\t\n"}), anyText())
}

// anyInstant draws an instant, usually in one of four seconds so instants tie, at a random
// precision so fractions land on and beside their boundaries. Years 0001–9998 leave room for an
// offset and an interval.
func anyInstant() *rapid.Generator[time.Time] {
	near := time.Date(2026, time.September, 11, 3, 14, 7, 0, time.UTC).Unix()
	first := time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	last := time.Date(9998, time.December, 31, 0, 0, 0, 0, time.UTC).Unix()

	return rapid.Custom(func(t *rapid.T) time.Time {
		sec := rapid.OneOf(rapid.Int64Range(near, near+3), rapid.Int64Range(first, last)).Draw(t, "sec")
		unit := rapid.SampledFrom([]int64{1, 1e3, 1e6, 1e9}).Draw(t, "unit")
		nsec := rapid.Int64Range(0, int64(time.Second)/unit-1).Draw(t, "units") * unit
		if rapid.Bool().Draw(t, "last in its unit") {
			nsec = int64(time.Second) - unit
		}

		return time.Unix(sec, nsec).UTC()
	})
}
