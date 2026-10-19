package cli_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// Plain test, not a Ginkgo spec: rapid keys its repro hint and fail files on the test name.
// Races and --id conflicts are excluded: each check runs alone against its own database.
func TestDryRunMeansWrite(t *testing.T) {
	runs := t.TempDir()
	check := func(t interface{ Context() context.Context }, args []string) error {
		dir, err := os.MkdirTemp(runs, "run")
		if err != nil {
			return err
		}
		db := filepath.Join(dir, "coroner.db")
		err = seedDryRunStore(t.Context(), db)
		if err != nil {
			return err
		}
		err = checkDryRunMeansWrite(t, coroner{home: dir, now: time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC)}, db, dryRunWriteOf(args))
		if err != nil {
			return errors.New(strings.ReplaceAll(err.Error(), dir, "$RUN"))
		}

		return os.RemoveAll(dir)
	}

	for _, c := range dryRunCases() {
		t.Run(c.name, func(t *testing.T) {
			err := check(t, c.args)
			if err != nil {
				t.Fatal(err)
			}
		})
	}

	if t.Failed() {
		return
	}
	rapid.Check(t, func(t *rapid.T) {
		args := rapid.OneOf(
			eventWrite(), annotationWrite(), linkWrite(), timelineWrite(), deleteWrite(), overlayWrite(), sourceWrite(),
		).Draw(t, "write")
		err := check(t, args)
		if err != nil {
			t.Fatal(err)
		}
	})
}

// dryRunCases reaches every write command's write, and the failures each must report alike in
// its dry run and its write.
func dryRunCases() []struct {
	name string
	args []string
} {
	return []struct {
		name string
		args []string
	}{
		{"timeline create", []string{"timeline", "create", "incident"}},
		{"timeline edit", []string{"timeline", "edit", "standby", "--name", "replica", "--title", "Standby site"}},
		{"timeline delete", []string{"timeline", "delete", "standby", "--yes"}},
		{"timeline use", []string{"timeline", "use", "standby"}},
		{"timeline use --clear", []string{"timeline", "use", "--clear"}},
		{"event add", []string{"event", "add", "Primary elected", "-t", "outage", "--tag", "phase:detect"}},
		{"event edit", []string{"event", "edit", "2", "-t", "outage", "--text", "Election won"}},
		{"event delete", []string{"event", "delete", "2", "-t", "outage"}},
		{"event link", []string{"event", "link", "2", "1", "-t", "outage", "--kind", "same_as"}},
		{"event unlink", []string{"event", "unlink", "1", "2", "-t", "outage", "--kind", "led_to"}},
		{"annotation add", []string{"annotation", "add", "Step-down", "-t", "outage", "--kind", "comment", "-e", "1"}},
		{"annotation edit", []string{"annotation", "edit", seededAnnotationID, "--body", "Burst first"}},
		{"annotation delete", []string{"annotation", "delete", seededAnnotationID}},
		{"attachment delete", []string{"attachment", "delete", seededAttachmentID}},
		{"overlay create", []string{"overlay", "create", "pair", "-t", "outage", "-t", "standby"}},
		{"source edit", []string{"source", "edit", "mongod", "--name", "mongo"}},

		{"timeline create taken name", []string{"timeline", "create", "standby"}},
		{"timeline edit taken name", []string{"timeline", "edit", "standby", "--name", "outage"}},
		{"timeline delete missing timeline", []string{"timeline", "delete", "incident", "--yes"}},
		{"event delete missing event", []string{"event", "delete", "9", "-t", "outage"}},
		{"event link existing link", []string{"event", "link", "1", "2", "-t", "outage", "--kind", "led_to"}},
		{"annotation add missing event", []string{"annotation", "add", "Step-down", "-t", "outage", "--kind", "comment", "-e", "99"}},
		{"annotation delete missing annotation", []string{"annotation", "delete", missingIDPrefix}},
		{"attachment delete missing attachment", []string{"attachment", "delete", missingIDPrefix}},
		{"overlay create taken name", []string{"overlay", "create", "both", "-t", "outage"}},
		{"source edit taken name", []string{"source", "edit", "mongod", "--name", "slack"}},
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
	seededTimelineID2  = "0199a1b2-0000-7000-8000-000000000007"
	seededLinkID       = "0199a1b2-0000-7000-8000-000000000008"
	seededAttachmentID = "0199a1b2-0000-7000-8000-000000000009"
	seededOverlayID    = "0199a1b2-0000-7000-8000-00000000000a"

	missingIDPrefix = "0199ffff"
)

// seedDryRunStore writes the timelines outage and standby, the overlay both of them, and in
// outage events 1 (source mongod, one attachment) and 2 (source slack), a led_to link from 1 to 2
// and an annotation.
func seedDryRunStore(ctx context.Context, db string) error {
	created := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)
	first := dryRunEvent(seededEventID, "Primary stepped down", created)
	first.Source = new("mongod")
	second := dryRunEvent(seededEventID2, "Election timeout", created.Add(time.Minute))
	second.Source = new("slack")
	err := seedStore(ctx, db, model{
		timelines: []timeline.Timeline{
			{ID: seededTimelineID, Name: "outage", Refs: []string{}, CreatedAt: created, UpdatedAt: created},
			{ID: seededTimelineID2, Name: "standby", Refs: []string{}, CreatedAt: created, UpdatedAt: created},
		},
		events: []eventDraft{
			{Event: first, newSourceID: seededSourceID, attachments: []attachmentDraft{
				{id: seededAttachmentID, name: "p99.txt", created: created, content: []byte("p99 at 4s\n")},
			}},
			{Event: second, newSourceID: seededSourceID2},
		},
		annotations: []annotationDraft{{
			ID: seededAnnotationID, TimelineID: seededTimelineID, Kind: timeline.AnnotationComment,
			Body: "Step-down follows the burst", CreatedAt: created, UpdatedAt: created,
		}},
		links: []timeline.Link{{ID: seededLinkID, FromEventID: seededEventID, ToEventID: seededEventID2, Kind: timeline.LinkLedTo, CreatedAt: created}},
	})
	if err != nil {
		return err
	}
	s, err := store.Open(ctx, db)
	if err != nil {
		return err
	}
	err = s.CreateOverlay(ctx, timeline.Overlay{ID: seededOverlayID, Name: "both", TimelineIDs: []string{seededTimelineID, seededTimelineID2}, CreatedAt: created})

	return errors.Join(err, s.Close())
}

// dryRunWrite is a write command's arguments, and how to check what its write stored.
type dryRunWrite struct {
	args []string
	// readBack returns the command that shows what the write did, given the dry run's preview.
	readBack func(want map[string]any) []string
	// expect returns an error unless readBack's result shows the write did what preview previewed.
	expect func(preview string, got commandResult) error
}

type commandResult struct {
	out, errOut string
	code        int
}

// dryRunWriteOf returns how to check the write command args runs.
func dryRunWriteOf(args []string) dryRunWrite {
	w := dryRunWrite{args: args}
	switch strings.Join(args[:2], " ") {
	case "event add", "event edit":
		w.readBack = func(want map[string]any) []string { return []string{"event", "view", fmt.Sprint(want["id"]), "--json"} }
		w.expect = storedAsPreviewed(pickWhole)
	case "annotation add", "annotation edit":
		w.readBack = func(map[string]any) []string { return []string{"-t", "outage", "annotation", "list", "--json"} }
		w.expect = storedAsPreviewed(pickListed)
	case "event link":
		w.readBack = func(want map[string]any) []string {
			return []string{"event", "view", fmt.Sprint(want["from_event_id"]), "--json"}
		}
		w.expect = storedAsPreviewed(pickListed)
	case "event unlink":
		w.readBack = func(want map[string]any) []string {
			return []string{"event", "view", fmt.Sprint(want["from_event_id"]), "--json"}
		}
		w.expect = unlinked
	case "timeline create", "timeline edit":
		w.readBack = func(want map[string]any) []string {
			return []string{"timeline", "view", fmt.Sprint(want["name"]), "--json"}
		}
		w.expect = storedAsPreviewed(pickWhole)
	case "timeline delete":
		w.readBack = func(want map[string]any) []string {
			return []string{"timeline", "view", fmt.Sprint(want["id"]), "--json"}
		}
		w.expect = notFound("timeline_not_found")
	case "event delete":
		w.readBack = func(want map[string]any) []string {
			return []string{"event", "view", listedIDs(want, "events")[0], "--json"}
		}
		w.expect = notFound("event_not_found")
	case "annotation delete":
		w.readBack = func(map[string]any) []string { return []string{"annotation", "list", "-t", "outage", "--json"} }
		w.expect = noneListed("annotations")
	case "attachment delete":
		w.readBack = func(map[string]any) []string { return []string{"attachment", "list", "-t", "outage", "--json"} }
		w.expect = noneListed("attachments")
	case "timeline use":
		w.readBack = func(map[string]any) []string { return []string{"status", "--json"} }
		w.expect = currentAsPreviewed
	case "overlay create":
		w.readBack = func(want map[string]any) []string {
			return []string{"event", "list", "--overlay", fmt.Sprint(want["name"]), "--json"}
		}
		w.expect = overlaidAsPreviewed
	case "source edit":
		w.readBack = func(map[string]any) []string { return []string{"source", "list", "--json"} }
		w.expect = renamedFrom(args[2])
	}

	return w
}

func dryRunEvent(id, text string, at time.Time) timeline.Event {
	return timeline.Event{
		ID: id, TimelineID: seededTimelineID, OccurredAt: at, Precision: timeline.PrecisionSecond,
		Tags: []string{}, Text: text, CreatedAt: at, UpdatedAt: at,
	}
}

// checkDryRunMeansWrite runs w with --dry-run --json, then without --dry-run, returning an error
// unless both fail with the same exit code and error kind, or both exit 0 and the write did what
// the dry run previewed.
func checkDryRunMeansWrite(t interface{ Context() context.Context }, c coroner, db string, w dryRunWrite) error {
	args := append([]string{"--db", db, "--no-input"}, w.args...)
	line := "coroner " + strings.Join(args, " ")
	preview, dryErr, dryCode := c.exec(t.Context(), append(args, "--dry-run", "--json")...)
	if strings.Contains(dryErr, "unknown flag") || strings.Contains(dryErr, "unknown shorthand flag") {
		return fmt.Errorf("%s: the generator drew a flag the command lacks:\n%s", line, dryErr)
	}
	_, writeErr, writeCode := c.exec(t.Context(), append(args, "--json")...)
	if dryCode != 0 {
		if writeCode != dryCode || errorKind(writeErr) != errorKind(dryErr) {
			return fmt.Errorf("%s: the dry run exited %d and the write exited %d\ndry run stderr:\n%s\nwrite stderr:\n%s", line, dryCode, writeCode, dryErr, writeErr)
		}

		return nil
	}
	if writeCode != 0 {
		return fmt.Errorf("%s: the dry run exited 0 and the write exited %d\ndry run stderr:\n%s\nwrite stderr:\n%s", line, writeCode, dryErr, writeErr)
	}

	var want map[string]any
	err := json.Unmarshal([]byte(preview), &want)
	if err != nil {
		return fmt.Errorf("%s: reading the preview: %w", line, err)
	}
	readBack := w.readBack(want)
	out, errOut, code := c.exec(t.Context(), append([]string{"--db", db}, readBack...)...)
	err = w.expect(preview, commandResult{out, errOut, code})
	if err != nil {
		return fmt.Errorf("%s, then %s: %w", line, strings.Join(readBack, " "), err)
	}

	return nil
}

// errorKind returns the kind in the JSON error envelope that ends stderr, or "".
func errorKind(stderr string) string {
	var envelope struct {
		Error struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &envelope)

	return envelope.Error.Kind
}

// storedAsPreviewed expects the record pick finds in the read back to be the previewed one.
func storedAsPreviewed(pick func(out string, want map[string]any) (map[string]any, error)) func(string, commandResult) error {
	return func(preview string, got commandResult) error {
		if got.code != 0 {
			return fmt.Errorf("exited %d:\n%s", got.code, got.errOut)
		}
		var want map[string]any
		err := json.Unmarshal([]byte(preview), &want)
		if err != nil {
			return err
		}
		stored, err := pick(got.out, want)
		if err != nil {
			return fmt.Errorf("reading the stored record: %w", err)
		}
		want, stored = comparableRecord(want), comparableRecord(stored)
		if !reflect.DeepEqual(stored, want) {
			return fmt.Errorf("the stored record differs from the preview\npreview: %v\nstored:  %v", want, stored)
		}

		return nil
	}
}

// notFound expects the read back to fail with kind.
func notFound(kind string) func(string, commandResult) error {
	return func(_ string, got commandResult) error {
		if got.code == 0 || errorKind(got.errOut) != kind {
			return fmt.Errorf("want %s, got exit %d:\n%s%s", kind, got.code, got.out, got.errOut)
		}

		return nil
	}
}

// noneListed expects no record the preview lists under key to be listed under key in the read back.
func noneListed(key string) func(string, commandResult) error {
	return func(preview string, got commandResult) error {
		var previewed, listed map[string]any
		err := errors.Join(json.Unmarshal([]byte(preview), &previewed), json.Unmarshal([]byte(got.out), &listed))
		if err != nil {
			return err
		}
		for _, id := range listedIDs(previewed, key) {
			if slices.Contains(listedIDs(listed, key), id) {
				return fmt.Errorf("%s is still listed:\n%s", id, got.out)
			}
		}

		return nil
	}
}

// unlinked expects the from event's read back to have no link of the previewed kind to its end.
func unlinked(preview string, got commandResult) error {
	var want map[string]any
	var event struct {
		Links []map[string]any `json:"links"`
	}
	err := errors.Join(json.Unmarshal([]byte(preview), &want), json.Unmarshal([]byte(got.out), &event))
	if err != nil {
		return err
	}
	for _, l := range event.Links {
		if l["from_event_id"] == want["from_event_id"] && l["to_event_id"] == want["to_event_id"] && l["kind"] == want["kind"] {
			return fmt.Errorf("the link is still there:\n%s", got.out)
		}
	}

	return nil
}

// currentAsPreviewed expects status to name the previewed timeline as current, or none for null.
func currentAsPreviewed(preview string, got commandResult) error {
	var want, status struct {
		ID       string `json:"id"`
		Timeline *struct {
			ID string `json:"id"`
		} `json:"timeline"`
	}
	err := errors.Join(json.Unmarshal([]byte(preview), &want), json.Unmarshal([]byte(got.out), &status))
	if err != nil {
		return err
	}
	current := ""
	if status.Timeline != nil {
		current = status.Timeline.ID
	}
	if current != want.ID {
		return fmt.Errorf("want current timeline %q:\n%s", want.ID, got.out)
	}

	return nil
}

// overlaidAsPreviewed expects the overlay's events to list exactly the previewed timelines, and
// every event to belong to one of them.
func overlaidAsPreviewed(preview string, got commandResult) error {
	if got.code != 0 {
		return fmt.Errorf("exited %d:\n%s", got.code, got.errOut)
	}
	var want struct {
		Timelines []string `json:"timelines"`
	}
	var listed struct {
		Timelines []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"timelines"`
		Events []struct {
			TimelineID string `json:"timeline_id"`
		} `json:"events"`
	}
	err := errors.Join(json.Unmarshal([]byte(preview), &want), json.Unmarshal([]byte(got.out), &listed))
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	var names []string
	for _, t := range listed.Timelines {
		ids[t.ID] = true
		names = append(names, t.Name)
	}
	slices.Sort(names)
	slices.Sort(want.Timelines)
	if !slices.Equal(names, want.Timelines) {
		return fmt.Errorf("want timelines %v:\n%s", want.Timelines, got.out)
	}
	for _, e := range listed.Events {
		if !ids[e.TimelineID] {
			return fmt.Errorf("an event of timeline %s is listed:\n%s", e.TimelineID, got.out)
		}
	}

	return nil
}

// renamedFrom expects the previewed name, and not old unless it is the same, to be listed.
func renamedFrom(old string) func(string, commandResult) error {
	return func(preview string, got commandResult) error {
		var want struct {
			Name string `json:"name"`
		}
		var listed struct {
			Sources []struct {
				Name string `json:"name"`
			} `json:"sources"`
		}
		err := errors.Join(json.Unmarshal([]byte(preview), &want), json.Unmarshal([]byte(got.out), &listed))
		if err != nil {
			return err
		}
		has := func(name string) bool {
			return slices.ContainsFunc(listed.Sources, func(s struct {
				Name string `json:"name"`
			},
			) bool {
				return s.Name == name
			})
		}
		if !has(want.Name) || (has(old) && old != want.Name) {
			return fmt.Errorf("want %q listed and %q not:\n%s", want.Name, old, got.out)
		}

		return nil
	}
}

// listedIDs returns the ids of the records listed under key.
func listedIDs(list map[string]any, key string) []string {
	records, _ := list[key].([]any)
	ids := make([]string, 0, len(records))
	for _, r := range records {
		record, _ := r.(map[string]any)
		ids = append(ids, fmt.Sprint(record["id"]))
	}

	return ids
}

// comparableRecord returns record without what a dry run cannot know or a read adds.
func comparableRecord(record map[string]any) map[string]any {
	maps.DeleteFunc(record, func(key string, _ any) bool {
		return key == "seq" || key == "created_at" || key == "updated_at" || key == "attachments" || key == "links"
	})

	return record
}

func pickWhole(out string, _ map[string]any) (map[string]any, error) {
	var record map[string]any
	return record, json.Unmarshal([]byte(out), &record)
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
func eventWrite() *rapid.Generator[[]string] {
	return rapid.Custom(eventArgs)
}

// annotationWrite draws annotation add or annotation edit of the seeded annotation.
func annotationWrite() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		args := []string{"annotation", "add", mostlyValid(t, "body", dryRunText()), "-t", "outage"}
		args = requiredFlag(t, args, "--kind", dryRunAnnotationKinds())
		args = optionalFlag(t, args, "-e", []string{"1", "2", seededEventID, "99"})
		args = optionalFlag(t, args, "--author", dryRunText())
		if rapid.Bool().Draw(t, "edit") {
			args = []string{"annotation", "edit", seededAnnotationID}
			args = optionalFlag(t, args, "--body", dryRunText())
			args = optionalFlag(t, args, "--kind", dryRunAnnotationKinds())
		}
		args = optionalFlag(t, args, "--source", []string{"mongod", "Mongo D", "-bad", "none", ""})

		return optionalFlag(t, args, "--url", dryRunText())
	})
}

// linkWrite draws event link or event unlink between the seeded events, a self link among them.
func linkWrite() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		ends := []string{"1", "2", seededEventID}
		verb := rapid.SampledFrom([]string{"link", "unlink"}).Draw(t, "verb")
		args := []string{"event", verb, rapid.SampledFrom(ends).Draw(t, "from"), rapid.SampledFrom(ends).Draw(t, "to"), "-t", "outage"}
		args = requiredFlag(t, args, "--kind", []string{"led_to", "same_as", "copied_from", "LED_TO", "caused", ""})
		if verb == "link" {
			args = optionalFlag(t, args, "--author", dryRunText())
		}

		return args
	})
}

// timelineWrite draws timeline create, edit or use, with names that are free, taken or invalid.
func timelineWrite() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		names := []string{"incident", "standby", "outage", "Bad Name", ""}
		switch rapid.SampledFrom([]string{"create", "edit", "use"}).Draw(t, "verb") {
		case "create":
			return optionalFlag(t, []string{"timeline", "create", mostlyValid(t, "name", names)}, "--title", dryRunText())
		case "edit":
			args := []string{"timeline", "edit", rapid.SampledFrom([]string{"standby", "outage", "incident"}).Draw(t, "timeline")}
			args = optionalFlag(t, args, "--name", names)
			args = optionalFlag(t, args, "--title", dryRunText())

			return optionalFlag(t, args, "--add-ref", []string{"INC-4822", "", "https://example.com/inc/1"})
		default:
			return []string{"timeline", "use", rapid.SampledFrom([]string{"standby", "outage", "incident", "--clear"}).Draw(t, "timeline")}
		}
	})
}

// deleteWrite draws a delete of a seeded record or of one that does not exist.
func deleteWrite() *rapid.Generator[[]string] {
	return rapid.SampledFrom([][]string{
		{"timeline", "delete", "standby", "--yes"},
		{"timeline", "delete", "outage", "--yes"},
		{"timeline", "delete", "incident", "--yes"},
		{"event", "delete", "1", "-t", "outage"},
		{"event", "delete", "2", "-t", "outage"},
		{"event", "delete", "9", "-t", "outage"},
		{"annotation", "delete", seededAnnotationID},
		{"annotation", "delete", missingIDPrefix},
		{"attachment", "delete", seededAttachmentID},
		{"attachment", "delete", missingIDPrefix},
	})
}

// overlayWrite draws overlay create with a free, taken or invalid name over known and unknown timelines.
func overlayWrite() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		args := []string{"overlay", "create", mostlyValid(t, "name", []string{"pair", "both", "Bad Name", ""})}
		for _, name := range rapid.SliceOfN(rapid.SampledFrom([]string{"outage", "standby", "incident"}), 1, 3).Draw(t, "timelines") {
			args = append(args, "-t", name)
		}

		return args
	})
}

// sourceWrite draws source edit of a seeded or unknown source, to a free, taken or invalid name.
func sourceWrite() *rapid.Generator[[]string] {
	return rapid.Custom(func(t *rapid.T) []string {
		from := rapid.SampledFrom([]string{"mongod", "slack", "nope"}).Draw(t, "source")
		return requiredFlag(t, []string{"source", "edit", from}, "--name", []string{"mongo", "slack", "mongod", "Mongo D", ""})
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
