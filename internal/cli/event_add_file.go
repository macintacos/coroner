package cli

import (
	"context"
	"fmt"
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
	events, err := readEventRecords(jsonlSource{path: path, readFile: app.ReadFile, stdin: app.In, stdinIsTTY: app.InIsTTY})
	if err != nil {
		return err
	}
	records := make([]export.Record, len(events))
	for i, e := range events {
		records[i] = e
	}

	dryRun, _ := in.boolean("dry-run")
	mode := mergeWrite
	if dryRun {
		mode = mergePlanOnly
	}
	input := mergeInput{
		records: records, onConflict: onConflictFail, newID: app.NewID, now: app.Now(),
		timelines: timelineInputOf(in), lookupEnv: app.LookupEnv,
	}
	var added eventsAdded
	err = withStorePath(ctx, app, inv.cfg.DBPath, func(s *store.Store) error {
		out, err := merge(ctx, s, input, mode)
		switch {
		case err != nil:
			return err
		case len(out.plan.problems) > 0:
			return invalidImportError(out.plan.problems, fromFileInvalidHint)
		case out.plan.blocked != blockNone:
			return conflictingEventsError(out.plan.result.Conflicts)
		}

		added = newEventsAdded(events, out.plan.identical, out.inserted)
		added.timeline, added.dryRun, added.dbPath = *out.guard, dryRun, inv.cfg.DBPath
		return s.Read(ctx, func(tx *store.Tx) error {
			added.attached, err = eventsAttachments(ctx, tx, added.identical)
			return err
		})
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

// readEventRecords returns the event records src names, rejecting any other record type and a
// file with none.
func readEventRecords(src jsonlSource) ([]export.Event, error) {
	if src.path == "" {
		return nil, &Error{Kind: KindUsage, Message: "--from-file needs a file", Hint: "Pass a JSONL file of event records, or - to read them from stdin."}
	}
	records, err := readImportRecords(src, fromFileInvalidHint)
	if err != nil {
		return nil, err
	}
	events, problems := eventRecords(records)
	switch {
	case len(problems) > 0:
		return nil, invalidImportError(problems, fromFileInvalidHint)
	case len(events) == 0 && src.path == "-":
		return nil, &Error{Kind: KindInvalidInput, Message: "stdin holds no event records", Hint: fromFileHint}
	case len(events) == 0:
		return nil, &Error{Kind: KindInvalidInput, Message: src.path + " holds no event records", Hint: fromFileHint}
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

const fromFileInvalidHint = "Nothing was added. Fix those records and run the command again."

// conflictingEventsError returns id_conflict listing the first few conflicts and how many more
// there are.
func conflictingEventsError(conflicts []importConflict) *Error {
	header := "1 event is already stored with different content, so nothing was added:"
	if len(conflicts) != 1 {
		header = fmt.Sprintf("%d events are already stored with different content, so nothing was added:", len(conflicts))
	}
	differences := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		differences = append(differences, fmt.Sprintf("event %s differs in %s", c.ID, strings.Join(c.Fields, ", ")))
	}
	lines := append([]string{header}, firstFew(differences, shownImportProblems)...)

	return &Error{
		Kind: KindIDConflict, Message: strings.Join(lines, "\n"),
		Hint: "Give those records new ids to add them as other events, or change the stored events with 'coroner event edit'.",
	}
}

// eventsAttachments returns the stored attachments of events.
// ponytail: one read per event; read the timeline's attachments at once if large re-runs read slowly.
func eventsAttachments(ctx context.Context, s records, events []timeline.Event) ([]timeline.Attachment, error) {
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

// newEventsAdded sorts events, each id once in file order, into the ones identical holds and the
// ones inserted holds; every event identical does not hold must be in inserted.
func newEventsAdded(events []export.Event, identical, inserted []timeline.Event) eventsAdded {
	identicalByID, insertedByID := eventsByID(identical), eventsByID(inserted)
	var added eventsAdded
	seen := make(map[string]bool, len(events))
	for _, e := range events {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		added.ids = append(added.ids, e.ID)
		if stored, ok := identicalByID[e.ID]; ok {
			added.identical = append(added.identical, stored)
			continue
		}
		added.inserted = append(added.inserted, insertedByID[e.ID])
	}

	return added
}

func eventsByID(events []timeline.Event) map[string]timeline.Event {
	byID := make(map[string]timeline.Event, len(events))
	for _, e := range events {
		byID[e.ID] = e
	}

	return byID
}

// eventsAddedJSON is event add --from-file's result object: every key always present.
type eventsAddedJSON struct {
	DryRun bool `json:"dry_run"`
	// Timelines resolves every event's timeline_id.
	Timelines []export.Timeline `json:"timelines"`
	// Inserted are the events added, or that would be; Identical those already stored unchanged.
	Inserted  []eventJSON `json:"inserted"`
	Identical []eventJSON `json:"identical"`
}

func (a eventsAdded) render(o output) error {
	if o.format == formatJSON {
		attached := attachmentsByOwner(a.attached, a.dbPath)
		err := o.json(eventsAddedJSON{
			DryRun: a.dryRun, Timelines: []export.Timeline{newTimelineJSON(a.timeline)},
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
