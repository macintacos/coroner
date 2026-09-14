package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const fromFileUsage = "Add every event record in this JSONL `file`, or - to read them from stdin, in one transaction"

// addEventsFromFile runs event add --from-file: it adds the event records --from-file names to
// the selected timeline in one transaction, as the insert-only subset of import, reading and
// planning them with import's decoder and planner.
func addEventsFromFile(ctx context.Context, cmd *cobra.Command, app App, args []string) error {
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	in := eventAddDefinition().read(cmd.Flags(), args)
	interactive, _ := cmd.Flags().GetBool("interactive")
	err = checkFromFileAlone(in, interactive)
	if err != nil {
		return err
	}
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return err
	}
	path, _ := cmd.Flags().GetString("from-file")
	records, err := readEventRecords(path, app)
	if err != nil {
		return err
	}

	dryRun, _ := in.boolean("dry-run")
	added := eventsAdded{dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
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
		if !dryRun {
			err = importWriteError(s.Import(ctx, plan.write))
			if err != nil {
				return err
			}
		}

		added, err = readAddedEvents(ctx, s, eventsAdded{timeline: t, dryRun: dryRun}, records, plan, cfg.DBPath)
		return err
	})
	if err != nil {
		return err
	}

	return added.render(o)
}

// checkFromFileAlone returns usage when in, or --interactive, gives a value --from-file's records
// replace: anything but -t, --tz and --dry-run.
func checkFromFileAlone(in fieldValues, interactive bool) error {
	var given []string
	for _, f := range eventAddDefinition().fields {
		switch {
		case !in.given(f.name) || slices.Contains([]string{"timeline", "tz", "dry-run"}, f.name):
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
func readEventRecords(path string, app App) ([]export.Record, error) {
	records, err := readImportRecords(path, app.ReadFile, app.In, app.InIsTTY)
	if err != nil {
		return nil, withFromFileHint(err)
	}
	events, problems := eventRecords(records)
	switch {
	case len(problems) > 0:
		return nil, withFromFileHint(invalidImportError(problems))
	case len(events) == 0:
		return nil, &Error{Kind: KindInvalidInput, Message: path + " holds no event records", Hint: fromFileHint}
	}

	return events, nil
}

// eventRecords returns records' events, skipping headers, and a problem naming each record of
// another type.
func eventRecords(records []export.Record) ([]export.Record, []string) {
	var events []export.Record
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
		}
	}

	return events, problems
}

const fromFileHint = "Nothing was added. Pass a JSONL file of event records, one per line, as 'coroner timeline export --format jsonl' writes them."

// withFromFileHint words a usage or invalid_input error from import's reader or planner for
// --from-file, and returns any other error as it is.
func withFromFileHint(err error) error {
	typed, ok := errors.AsType[*Error](err)
	if !ok || (typed.Kind != KindUsage && typed.Kind != KindInvalidInput) {
		return err
	}
	hinted := *typed
	hinted.Message = strings.Replace(typed.Message, "cannot be imported", "cannot be added", 1)
	hinted.Hint = fromFileHint

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

// eventsAdded is what --from-file added, or would add, and found already stored, in file order.
type eventsAdded struct {
	timeline            timeline.Timeline
	inserted, identical []eventJSON
	// ids are every event's id in file order, each once.
	ids    []string
	dryRun bool
}

// readAddedEvents returns added with the objects of the events records gives, read back from s
// once plan has been written, or as plan would write them in a dry run.
// ponytail: one read per event; read a timeline's events at once if large batches read slowly.
func readAddedEvents(
	ctx context.Context, s *store.Store, added eventsAdded, records []export.Record, plan importPlan, dbPath string,
) (eventsAdded, error) {
	planned := map[string]timeline.Event{}
	for _, e := range plan.write.Events {
		e.Seq = 0
		planned[e.ID] = e
	}
	added.inserted, added.identical = []eventJSON{}, []eventJSON{}
	for _, r := range records {
		record, isEvent := r.(export.Event)
		id := record.ID
		if !isEvent || slices.Contains(added.ids, id) {
			continue
		}
		added.ids = append(added.ids, id)

		e, isNew := planned[id]
		if !isNew || !added.dryRun {
			var err error
			e, err = s.Event(ctx, id)
			if err != nil {
				return added, err
			}
		}
		attachments, err := s.ListAttachments(ctx, timeline.AttachmentOwner{EventID: &id})
		if err != nil {
			return added, err
		}
		object := newEventJSON(e, newAttachmentJSONs(attachments, dbPath))
		if isNew {
			added.inserted = append(added.inserted, object)
		} else {
			added.identical = append(added.identical, object)
		}
	}

	return added, nil
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
		err := o.json(eventsAddedJSON{
			DryRun: a.dryRun, Timelines: []timelineJSON{newTimelineJSON(a.timeline)}, Inserted: a.inserted, Identical: a.identical,
		})
		if err != nil {
			return err
		}
	}
	for _, id := range a.ids {
		if a.dryRun {
			break
		}
		err := o.printID(id)
		if err != nil {
			return err
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
