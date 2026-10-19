package cli_test

import (
	"encoding/json/v2"
	"errors"
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
	readBackCompared := map[string]bool{}

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
			annotations: []annotationDraft{{
				ID: seededAnnotationID, TimelineID: seededTimelineID, Kind: timeline.AnnotationComment,
				Body: "Step-down follows the burst", CreatedAt: created, UpdatedAt: created,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}

		write := rapid.OneOf(eventWrite(), annotationWrite(), linkWrite()).Draw(t, "write")
		compared, err := checkDryRunMeansWrite(t, coroner{home: dir, now: time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC)}, db, write)
		command := strings.Join(write.args[:2], " ")
		readBackCompared[command] = readBackCompared[command] || compared
		if err != nil {
			t.Fatal(strings.ReplaceAll(err.Error(), dir, "$RUN"))
		}
		err = os.RemoveAll(dir)
		if err != nil {
			t.Fatal(err)
		}
	})

	var missing []string
	for _, command := range []string{"event add", "event edit", "annotation add", "annotation edit", "event link"} {
		if !readBackCompared[command] {
			missing = append(missing, command)
		}
	}
	if enoughChecksToCover() && len(missing) > 0 {
		t.Errorf("no drawn %s reached the write", strings.Join(missing, ", "))
	}
}

// The seeded ids come from a different millisecond than any id a command draws.
const (
	seededTimelineID = "0199a1b2-0000-7000-8000-000000000001"
	seededEventID    = "0199a1b2-0000-7000-8000-000000000002"
	seededEventID2   = "0199a1b2-0000-7000-8000-000000000003"
	seededSourceID   = "0199a1b2-0000-7000-8000-000000000004"
	seededSourceID2  = "0199a1b2-0000-7000-8000-000000000005"

	seededAnnotationID = "0199a1b2-0000-7000-8000-000000000006"
)

// dryRunWrite is a write command's arguments, and how to read back the record it stores.
type dryRunWrite struct {
	args []string
	// readBack returns the command that shows the record want previews, and picks the record
	// from its JSON output.
	readBack func(want map[string]any) []string
	pick     func(out string, want map[string]any) (map[string]any, error)
}

func dryRunEvent(id, text string, at time.Time) timeline.Event {
	return timeline.Event{
		ID: id, TimelineID: seededTimelineID, OccurredAt: at, Precision: timeline.PrecisionSecond,
		Tags: []string{}, Text: text, CreatedAt: at, UpdatedAt: at,
	}
}

// checkDryRunMeansWrite runs w with --dry-run --json and, when that exits 0, without --dry-run,
// returning an error unless the write exits 0 and stores the record the dry run previewed. It
// reports whether the stored record was compared.
func checkDryRunMeansWrite(t *rapid.T, c coroner, db string, w dryRunWrite) (bool, error) {
	args := append([]string{"--db", db, "--no-input"}, w.args...)
	line := "coroner " + strings.Join(args, " ")
	preview, dryErr, dryCode := c.exec(t.Context(), append(args, "--dry-run", "--json")...)
	if strings.Contains(dryErr, "unknown flag") || strings.Contains(dryErr, "unknown shorthand flag") {
		return false, fmt.Errorf("%s: the generator drew a flag the command lacks:\n%s", line, dryErr)
	}
	if dryCode != 0 {
		return false, nil
	}
	_, writeErr, writeCode := c.exec(t.Context(), append(args, "--json")...)
	if writeCode != 0 {
		return false, fmt.Errorf("%s: the dry run exited 0 and the write exited %d\ndry run stderr:\n%s\nwrite stderr:\n%s", line, writeCode, dryErr, writeErr)
	}

	var want map[string]any
	err := json.Unmarshal([]byte(preview), &want)
	if err != nil {
		return false, fmt.Errorf("%s: reading the preview: %w", line, err)
	}
	readBack := w.readBack(want)
	stored, viewErr, viewCode := c.exec(t.Context(), append([]string{"--db", db}, readBack...)...)
	if viewCode != 0 {
		return false, fmt.Errorf("%s: %s exited %d:\n%s", line, strings.Join(readBack, " "), viewCode, viewErr)
	}
	got, err := w.pick(stored, want)
	if err != nil {
		return false, fmt.Errorf("%s: reading the stored record: %w", line, err)
	}
	want, got = comparableRecord(want), comparableRecord(got)
	if !reflect.DeepEqual(got, want) {
		return false, fmt.Errorf("%s: the stored record differs from the preview\npreview: %v\nstored:  %v", line, want, got)
	}

	return true, nil
}

// comparableRecord returns record without what a dry run cannot know or a read adds.
func comparableRecord(record map[string]any) map[string]any {
	maps.DeleteFunc(record, func(key string, _ any) bool {
		return key == "seq" || key == "created_at" || key == "updated_at" || key == "attachments" || key == "links"
	})

	return record
}

// pickListed returns the annotation or link in out whose id is want's.
func pickListed(out string, want map[string]any) (map[string]any, error) {
	var listed struct {
		Annotations []map[string]any `json:"annotations"`
		Links       []map[string]any `json:"links"`
	}
	err := json.Unmarshal([]byte(out), &listed)
	for _, record := range append(listed.Annotations, listed.Links...) {
		if record["id"] == want["id"] {
			return record, err
		}
	}

	return nil, errors.Join(err, fmt.Errorf("no record has id %v in:\n%s", want["id"], out))
}

// eventWrite draws event add or event edit with flag values that mix valid ones with edge cases.
func eventWrite() *rapid.Generator[dryRunWrite] {
	return rapid.Custom(func(t *rapid.T) dryRunWrite {
		return dryRunWrite{
			args: eventArgs(t),
			readBack: func(want map[string]any) []string {
				return []string{"event", "view", fmt.Sprint(want["id"]), "--json"}
			},
			pick: func(out string, _ map[string]any) (map[string]any, error) {
				var event map[string]any
				return event, json.Unmarshal([]byte(out), &event)
			},
		}
	})
}

// annotationWrite draws annotation add or annotation edit of the seeded annotation.
func annotationWrite() *rapid.Generator[dryRunWrite] {
	return rapid.Custom(func(t *rapid.T) dryRunWrite {
		args := []string{"annotation", "add", mostlyValid(t, "body", dryRunText()), "-t", "outage"}
		args = requiredFlag(t, args, "--kind", dryRunAnnotationKinds())
		args = optionalFlag(t, args, "-e", []string{"1", "2", seededEventID})
		args = optionalFlag(t, args, "--author", dryRunText())
		if rapid.Bool().Draw(t, "edit") {
			args = []string{"annotation", "edit", seededAnnotationID}
			args = optionalFlag(t, args, "--body", dryRunText())
			args = optionalFlag(t, args, "--kind", dryRunAnnotationKinds())
		}
		args = optionalFlag(t, args, "--source", []string{"mongod", "Mongo D", "-bad", "none", ""})
		args = optionalFlag(t, args, "--url", dryRunText())

		return dryRunWrite{
			args: args,
			readBack: func(map[string]any) []string {
				return []string{"-t", "outage", "annotation", "list", "--json"}
			},
			pick: pickListed,
		}
	})
}

// linkWrite draws event link between the seeded events, a self link among them.
func linkWrite() *rapid.Generator[dryRunWrite] {
	return rapid.Custom(func(t *rapid.T) dryRunWrite {
		ends := []string{"1", "2", seededEventID}
		args := []string{"event", "link", rapid.SampledFrom(ends).Draw(t, "from"), rapid.SampledFrom(ends).Draw(t, "to"), "-t", "outage"}
		args = requiredFlag(t, args, "--kind", []string{"led_to", "same_as", "copied_from", "LED_TO", "caused", ""})
		args = optionalFlag(t, args, "--author", dryRunText())

		return dryRunWrite{
			args: args,
			readBack: func(want map[string]any) []string {
				return []string{"event", "view", fmt.Sprint(want["from_event_id"]), "--json"}
			},
			pick: pickListed,
		}
	})
}

func dryRunAnnotationKinds() []string {
	return []string{"evidence", "comment", "Comment", "note", ""}
}

func eventArgs(t *rapid.T) []string {
	args := []string{"event", "add", mostlyValid(t, "text", dryRunText()), "-t", "outage"}
	if rapid.Bool().Draw(t, "edit") {
		args = []string{"event", "edit", rapid.SampledFrom([]string{"1", "2", seededEventID}).Draw(t, "ref"), "-t", "outage"}
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
}

func optionalFlag(t *rapid.T, args []string, flag string, pool []string) []string {
	if !rapid.Bool().Draw(t, flag) {
		return args
	}

	return requiredFlag(t, args, flag, pool)
}

func requiredFlag(t *rapid.T, args []string, flag string, pool []string) []string {
	return append(args, flag+"="+mostlyValid(t, flag+" value", pool))
}

// mostlyValid draws pool's first value, a valid one, half the time, so that a command drawing
// several values still often reaches its write.
func mostlyValid(t *rapid.T, label string, pool []string) string {
	return rapid.OneOf(rapid.Just(pool[0]), rapid.SampledFrom(pool)).Draw(t, label)
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
