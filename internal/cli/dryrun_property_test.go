package cli_test

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/timeline"
)

// Plain test, not a Ginkgo spec: rapid keys its repro hint and fail files on the test name.
// Races and --id conflicts are excluded: each check runs alone against its own database.
func TestDryRunMeansWrite(t *testing.T) {
	runs := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		dir, err := os.MkdirTemp(runs, "run")
		if err != nil {
			t.Fatal(err)
		}
		db := filepath.Join(dir, "coroner.db")
		created := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)
		err = seedStore(t.Context(), db, model{
			timelines: []timeline.Timeline{{ID: seededTimelineID, Name: "outage", Refs: []string{}, CreatedAt: created, UpdatedAt: created}},
			events: []eventDraft{
				{Event: dryRunEvent(seededEventID, "Primary stepped down", created), newSourceID: seededSourceID},
				{Event: dryRunEvent(seededEventID2, "Election timeout", created.Add(time.Minute)), newSourceID: seededSourceID2},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		args := append([]string{"--db", db, "-t", "outage", "--no-input"}, eventWrite().Draw(t, "command")...)
		err = checkDryRunMeansWrite(t, coroner{home: dir, now: time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC)}, args)
		if err != nil {
			t.Fatal(strings.ReplaceAll(err.Error(), dir, "$RUN"))
		}
		err = os.RemoveAll(dir)
		if err != nil {
			t.Fatal(err)
		}
	})
}

// The seeded ids come from a different millisecond than any id a command draws.
const (
	seededTimelineID = "0199a1b2-0000-7000-8000-000000000001"
	seededEventID    = "0199a1b2-0000-7000-8000-000000000002"
	seededEventID2   = "0199a1b2-0000-7000-8000-000000000003"
	seededSourceID   = "0199a1b2-0000-7000-8000-000000000004"
	seededSourceID2  = "0199a1b2-0000-7000-8000-000000000005"
)

func dryRunEvent(id, text string, at time.Time) timeline.Event {
	return timeline.Event{
		ID: id, TimelineID: seededTimelineID, OccurredAt: at, Precision: timeline.PrecisionSecond,
		Tags: []string{}, Text: text, CreatedAt: at, UpdatedAt: at,
	}
}

// checkDryRunMeansWrite runs args with --dry-run --json and, when that exits 0, without --dry-run,
// returning an error unless the write exits 0 and stores the event the dry run previewed.
func checkDryRunMeansWrite(t *rapid.T, c coroner, args []string) error {
	line := "coroner " + strings.Join(args, " ")
	preview, dryErr, dryCode := c.exec(t.Context(), append(args, "--dry-run", "--json")...)
	if dryCode != 0 {
		return nil
	}
	_, writeErr, writeCode := c.exec(t.Context(), append(args, "--json")...)
	if writeCode != 0 {
		return fmt.Errorf("%s: the dry run exited 0 and the write exited %d\ndry run stderr:\n%s\nwrite stderr:\n%s", line, writeCode, dryErr, writeErr)
	}

	want, err := comparableEvent(preview)
	if err != nil {
		return fmt.Errorf("%s: reading the preview: %w", line, err)
	}
	stored, viewErr, viewCode := c.exec(t.Context(), "--db", args[1], "event", "view", fmt.Sprint(want["id"]), "--json")
	if viewCode != 0 {
		return fmt.Errorf("%s: event view exited %d:\n%s", line, viewCode, viewErr)
	}
	got, err := comparableEvent(stored)
	if err != nil {
		return fmt.Errorf("%s: reading the stored event: %w", line, err)
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("%s: the stored event differs from the preview\npreview: %v\nstored:  %v", line, want, got)
	}

	return nil
}

// comparableEvent reads an event object, without what a dry run cannot know or a view adds.
func comparableEvent(out string) (map[string]any, error) {
	var event map[string]any
	err := json.Unmarshal([]byte(out), &event)
	maps.DeleteFunc(event, func(key string, _ any) bool {
		return key == "seq" || key == "created_at" || key == "updated_at" || key == "attachments" || key == "links"
	})

	return event, err
}

// eventWrite draws event add or event edit with flag values that mix valid ones with edge cases.
func eventWrite() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		args := []string{"event", "add", rapid.SampledFrom(dryRunText()).Draw(t, "text")}
		if rapid.Bool().Draw(t, "edit") {
			args = []string{"event", "edit", rapid.SampledFrom([]string{"1", "2", seededEventID}).Draw(t, "ref")}
			args = optionalFlag(t, args, "--text", dryRunText())
			args = optionalFlag(t, args, "--add-tag", dryRunTags())
			args = optionalFlag(t, args, "--remove-tag", dryRunTags())
		} else {
			args = optionalFlag(t, args, "--tag", dryRunTags())
		}
		args = optionalFlag(t, args, "--at", dryRunTimes())
		args = optionalFlag(t, args, "--ended", append(dryRunTimes(), "none"))
		args = optionalFlag(t, args, "--source", []string{"mongod", "slack", "Mongo D", "-bad", "none", ""})
		args = optionalFlag(t, args, "--url", dryRunText())
		args = optionalFlag(t, args, "--author", dryRunText())
		if rapid.Bool().Draw(t, "approximate") {
			args = append(args, "--approximate")
		}

		return args
	})
}

func optionalFlag(t *rapid.T, args []string, flag string, pool []string) []string {
	if !rapid.Bool().Draw(t, flag) {
		return args
	}

	return append(args, flag+"="+rapid.SampledFrom(pool).Draw(t, flag+" value"))
}

// dryRunTimes holds offsets at and past a day from UTC, years at and past the range's ends, and
// times before and after the seeded events, so an --ended can fall before --at.
func dryRunTimes() []string {
	return []string{
		"2026-09-11T03:14:30Z", "2026-09-11T02:00:00Z", "2026-09-12 14:00", "around 2026-09-11 14:00",
		"2026-09-11T03:14:30+24:00", "2026-09-11T03:14:30+24:30", "2026-09-11T03:14:30-24:59",
		"0000-01-01T00:00:00Z", "9999-12-31T23:59:59Z", "10000-01-01T00:00:00Z", "2026-09-11T03:14:30Z\xff",
	}
}

// dryRunText serves text, URLs and authors: valid values, blank ones, text that is not UTF-8 and none.
func dryRunText() []string {
	return []string{"Primary elected", "https://example.com/log/1", "Priya Shah", "", " ", "\t\n", "caf\xe9", "none"}
}

func dryRunTags() []string {
	return []string{"phase:detect", "db-3", "Bad Tag", "-x", "none", ""}
}
