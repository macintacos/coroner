package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const fromFileUsage = "Add every event record in this JSONL `file`, or - to read them from stdin, in one transaction"

// addEventsFromFile adds the event records in --from-file's file, or stdin for -, to the selected
// timeline in one transaction.
func addEventsFromFile(ctx context.Context, cmd *cobra.Command, app App, args []string) error {
	in := eventAddDefinition().read(cmd.Flags(), args)
	interactive, _ := cmd.Flags().GetBool("interactive")
	err := checkFromFileAlone(in, interactive)
	if err != nil {
		return err
	}
	inv, err := readInvocation(cmd, app)
	if err != nil {
		return err
	}
	path, _ := cmd.Flags().GetString("from-file")
	events, err := readEventRecords(path, app.ReadFile, app.In, app.InIsTTY)
	if err != nil {
		return err
	}
	records := make([]export.Record, len(events))
	for i, e := range events {
		records[i] = e
	}

	dryRun, _ := in.boolean("dry-run")
	var added eventsAdded
	err = withStorePath(ctx, app, inv.cfg.DBPath, func(s *store.Store) error {
		t, err := selectTimeline(ctx, s, in.value("timeline"), app.LookupEnv)
		if err != nil {
			return err
		}
		input := importInput{records: records, onConflict: onConflictFail, guard: &t, newID: app.NewID, now: app.Now()}
		input.stored, err = readStoredRecords(ctx, s, records)
		if err != nil {
			return err
		}
		plan, err := planImport(input)
		if err != nil {
			return withFromFileHint(err)
		}
		if plan.blocked != nil {
			return conflictingEventsError(plan.result.Conflicts)
		}

		inserted := plannedEvents(plan.write.Events)
		if !dryRun {
			inserted, err = writeEvents(ctx, s, plan.write, t.ID)
			if err != nil {
				return err
			}
		}
		added = newEventsAdded(events, input.stored.events, inserted)
		added.timeline, added.dryRun, added.dbPath = t, dryRun, inv.cfg.DBPath
		added.attached, err = eventsAttachments(ctx, s, added.identical)
		return err
	})
	if err != nil {
		return err
	}

	return added.render(inv.out)
}

// checkFromFileAlone returns usage when in, or --interactive, gives a value --from-file's records
// replace: anything but an inherited flag, such as -t or --tz, and --dry-run.
func checkFromFileAlone(in fieldValues, interactive bool) error {
	var given []string
	for _, f := range eventAddDefinition().fields {
		switch {
		case !in.given(f.name) || f.inherited || f.name == "dry-run":
		case f.arg:
			given = append(given, "event text")
		default:
			given = append(given, "--"+f.name)
		}
	}
	if interactive {
		given = append(given, "--interactive")
	}
	if len(given) == 0 {
		return nil
	}

	return &Error{
		Kind:    KindUsage,
		Message: "--from-file takes every field from its records, so it cannot be combined with " + strings.Join(given, ", "),
		Hint:    "Put those values in the records, or add one event without --from-file.",
	}
}

// readEventRecords returns the event records in the file at path, or on stdin for -, rejecting
// any other record type and a file with none.
func readEventRecords(
	path string, readFile func(string) ([]byte, error), stdin io.Reader, stdinIsTTY bool,
) ([]export.Event, error) {
	if path == "" {
		return nil, &Error{Kind: KindUsage, Message: "--from-file needs a file", Hint: "Pass a JSONL file of event records, or - to read them from stdin."}
	}
	records, err := readImportRecords(path, readFile, stdin, stdinIsTTY)
	if err != nil {
		return nil, withFromFileHint(err)
	}
	events, problems := eventRecords(records)
	switch {
	case len(problems) > 0:
		return nil, withFromFileHint(invalidImportError(problems))
	case len(events) == 0 && path == "-":
		return nil, &Error{Kind: KindInvalidInput, Message: "stdin holds no event records", Hint: fromFileHint}
	case len(events) == 0:
		return nil, &Error{Kind: KindInvalidInput, Message: path + " holds no event records", Hint: fromFileHint}
	}

	return events, nil
}

// eventRecords returns records' events, skipping headers, and a problem naming each record of
// another type.
func eventRecords(records []export.Record) ([]export.Event, []string) {
	var events []export.Event
	var problems []string
	notEvent := func(typ, id string) {
		problems = append(problems, fmt.Sprintf("%s %s: event add --from-file adds events only; coroner import reads %s records", typ, id, typ))
	}
	for _, r := range records {
		switch r := r.(type) {
		case export.Event:
			events = append(events, r)
		case export.Header:
		case export.Timeline:
			notEvent("timeline", r.ID)
		case export.Source:
			notEvent("source", r.ID)
		case export.Annotation:
			notEvent("annotation", r.ID)
		case export.Attachment:
			notEvent("attachment", r.ID)
		case export.Link:
			notEvent("link", r.ID)
		default:
			problems = append(problems, fmt.Sprintf("%T: event add --from-file adds events only", r))
		}
	}

	return events, problems
}

const fromFileHint = "Nothing was added. Pass a JSONL file of event records, one per line, as 'coroner timeline export --format jsonl' writes them."

// withFromFileHint gives an invalid_input error from import's reader or planner event add's hint,
// and returns any other error as it is.
func withFromFileHint(err error) error {
	typed, ok := errors.AsType[*Error](err)
	if !ok || typed.Kind != KindInvalidInput {
		return err
	}
	hinted := *typed
	hinted.Hint = "Nothing was added. Fix those records and run the command again."

	return &hinted
}

// conflictingEventsError returns id_conflict listing the first few conflicts and how many more
// there are.
func conflictingEventsError(conflicts []importConflict) *Error {
	header := "1 event is already stored with different content, so nothing was added:"
	if len(conflicts) != 1 {
		header = fmt.Sprintf("%d events are already stored with different content, so nothing was added:", len(conflicts))
	}
	lines := []string{header}
	for _, c := range conflicts[:min(len(conflicts), shownImportProblems)] {
		lines = append(lines, fmt.Sprintf("event %s differs in %s", c.ID, strings.Join(c.Fields, ", ")))
	}
	if more := len(conflicts) - shownImportProblems; more > 0 {
		lines = append(lines, fmt.Sprintf("and %d more", more))
	}

	return &Error{
		Kind: KindIDConflict, Message: strings.Join(lines, "\n"),
		Hint: "Give those records new ids to add them as other events, or change the stored events with 'coroner event edit'.",
	}
}

// plannedEvents returns the events a dry run would insert, by id.
func plannedEvents(events []timeline.Event) map[string]timeline.Event {
	planned := make(map[string]timeline.Event, len(events))
	for _, e := range events {
		// A dry run allocates no seq, so the preview prints null as event add --dry-run does.
		e.Seq = 0
		planned[e.ID] = e
	}

	return planned
}

// writeEvents writes records and returns the events of the timeline with timelineID by id, which
// now hold records' under the seqs the store gave them.
func writeEvents(ctx context.Context, s *store.Store, records store.ImportRecords, timelineID string) (map[string]timeline.Event, error) {
	err := s.Import(ctx, records)
	if errors.Is(err, store.ErrConflict) {
		return nil, &Error{
			Kind: KindIDConflict, Message: "the database changed while the events were being added, so nothing was added",
			Hint: "Run the command again, which skips the events already stored.", Err: err,
		}
	}
	if err != nil {
		return nil, err
	}
	events, err := s.Events(ctx, timelineID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]timeline.Event, len(events))
	for _, e := range events {
		byID[e.ID] = e
	}

	return byID, nil
}

// eventsAttachments returns the stored attachments of events.
// ponytail: one read per event; read the timeline's attachments at once if large re-runs read slowly.
func eventsAttachments(ctx context.Context, s *store.Store, events []timeline.Event) ([]timeline.Attachment, error) {
	owners := make([]timeline.AttachmentOwner, 0, len(events))
	for _, e := range events {
		owners = append(owners, timeline.AttachmentOwner{EventID: &e.ID})
	}

	return ownersAttachments(ctx, s, owners)
}

// eventsAdded is what --from-file added, or would add, and found already stored.
type eventsAdded struct {
	timeline            timeline.Timeline
	inserted, identical []timeline.Event
	// attached are the identical events' attachments; an inserted event has none.
	attached []timeline.Attachment
	// ids are every event's id in file order, each once.
	ids    []string
	dryRun bool
	dbPath string
}

// newEventsAdded sorts events, each id once in file order, into the identical ones stored holds
// and the inserted ones inserted holds.
func newEventsAdded(events []export.Event, stored, inserted map[string]timeline.Event) eventsAdded {
	var added eventsAdded
	seen := make(map[string]bool, len(events))
	for _, e := range events {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		added.ids = append(added.ids, e.ID)
		if identical, ok := stored[e.ID]; ok {
			added.identical = append(added.identical, identical)
		} else {
			added.inserted = append(added.inserted, inserted[e.ID])
		}
	}

	return added
}

// eventsAddedJSON is event add --from-file's result object: every key always present.
type eventsAddedJSON struct {
	DryRun bool `json:"dry_run"`
	// Timelines resolves every event's timeline_id.
	Timelines []timelineJSON `json:"timelines"`
	// Inserted are the events added, or that would be; Identical those already stored unchanged.
	Inserted  []eventJSON `json:"inserted"`
	Identical []eventJSON `json:"identical"`
}

func (a eventsAdded) render(o output) error {
	if o.format == formatJSON {
		attached := attachmentsByOwner(a.attached, a.dbPath)
		err := o.json(eventsAddedJSON{
			DryRun: a.dryRun, Timelines: []timelineJSON{newTimelineJSON(a.timeline)},
			Inserted: eventJSONs(a.inserted, attached), Identical: eventJSONs(a.identical, attached),
		})
		if err != nil {
			return err
		}
	}
	if !a.dryRun {
		for _, id := range a.ids {
			err := o.printID(id)
			if err != nil {
				return err
			}
		}
	}

	verb := "Added"
	if a.dryRun {
		verb = "Would add"
	}
	message := fmt.Sprintf("%s %d %s to timeline `%s`", verb, len(a.inserted), pluralNoun(len(a.inserted), "event"), a.timeline.Name)
	switch n := len(a.identical); {
	case n == 1:
		message += "; 1 was already there"
	case n > 1:
		message += fmt.Sprintf("; %d were already there", n)
	}

	return o.notify("%s", message)
}

// eventJSONs returns events' objects, each with its attachments in attached, by event id.
func eventJSONs(events []timeline.Event, attached map[string][]attachmentJSON) []eventJSON {
	objects := make([]eventJSON, 0, len(events))
	for _, e := range events {
		objects = append(objects, newEventJSON(e, attached[e.ID]))
	}

	return objects
}
