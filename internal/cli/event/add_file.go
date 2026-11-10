package event

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/merge"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const fromFileUsage = "Add every event record in this JSONL `file`, or - to read them from stdin, in one transaction"

// addEventsFromFile adds the event records in --from-file's file, or stdin for -, to the selected
// timeline in one transaction.
func addEventsFromFile(ctx context.Context, cmd *cobra.Command, app clikit.App, args []string) error {
	in := AddDefinition().Read(cmd.Flags(), args)
	interactive, _ := cmd.Flags().GetBool("interactive")
	err := checkFromFileAlone(in, interactive)
	if err != nil {
		return err
	}
	inv, err := clikit.ReadInvocation(cmd, app)
	if err != nil {
		return err
	}
	path, _ := cmd.Flags().GetString("from-file")
	events, err := readEventRecords(app.Logger(), merge.JSONLSource{Path: path, ReadFile: app.ReadFile, Stdin: app.In, StdinIsTTY: app.InIsTTY})
	if err != nil {
		return err
	}
	records := make([]export.Record, len(events))
	for i, e := range events {
		records[i] = e
	}

	dryRun, _ := in.Boolean("dry-run")
	mode := merge.Write
	if dryRun {
		mode = merge.PlanOnly
	}
	input := merge.Input{
		Records: records, OnConflict: merge.OnConflictFail, NewID: app.NewID, Now: app.Now(),
		Timelines: clikit.TimelineInputOf(in), LookupEnv: app.LookupEnv, Logger: app.Logger(),
	}
	var added eventsAdded
	var decisions []merge.ImportDecision
	err = clikit.WithStorePath(ctx, app, inv.Cfg.DBPath, func(s *store.Store) error {
		out, err := merge.Merge(ctx, s, input, mode)
		switch {
		case err != nil:
			return err
		case len(out.Plan.Problems) > 0:
			return merge.InvalidImportError(out.Plan.Problems, fromFileInvalidHint)
		case out.Plan.Blocked != merge.BlockNone:
			return conflictingEventsError(out.Plan.Result.Conflicts)
		}

		decisions = out.Plan.Decisions
		added = newEventsAdded(events, out.Plan.Identical, out.Inserted)
		added.timeline, added.dryRun, added.dbPath = *out.Guard, dryRun, inv.Cfg.DBPath
		return s.Read(ctx, func(tx *store.Tx) error {
			added.attached, err = eventsAttachments(ctx, tx, added.identical)
			return err
		})
	})
	if err != nil {
		return err
	}

	merge.LogImportPlan(ctx, clikit.WithDryRun(app.Logger(), dryRun), decisions)
	return added.Render(inv.Out)
}

// checkFromFileAlone returns usage when in, or --interactive, gives a value --from-file's records
// replace: anything but an inherited flag, such as -t or --tz, and --dry-run.
func checkFromFileAlone(in clikit.FieldValues, interactive bool) error {
	var given []string
	for _, f := range AddDefinition().Fields {
		switch {
		case !in.Given(f.Name) || f.Inherited || f.Name == "dry-run":
		case f.Arg:
			given = append(given, "event text")
		default:
			given = append(given, "--"+f.Name)
		}
	}
	if interactive {
		given = append(given, "--interactive")
	}
	if len(given) == 0 {
		return nil
	}

	return &clikit.Error{
		Kind:    clikit.KindUsage,
		Message: "--from-file takes every field from its records, so it cannot be combined with " + strings.Join(given, ", "),
		Hint:    "Put those values in the records, or add one event without --from-file.",
	}
}

// readEventRecords returns the event records src names, rejecting any other record type and a
// file with none.
func readEventRecords(logger *slog.Logger, src merge.JSONLSource) ([]export.Event, error) {
	if src.Path == "" {
		return nil, &clikit.Error{Kind: clikit.KindUsage, Message: "--from-file needs a file", Hint: "Pass a JSONL file of event records, or - to read them from stdin."}
	}
	records, err := merge.ReadImportRecords(logger, src, fromFileInvalidHint)
	if err != nil {
		return nil, err
	}
	events, problems := eventRecords(records)
	switch {
	case len(problems) > 0:
		return nil, merge.InvalidImportError(problems, fromFileInvalidHint)
	case len(events) == 0 && src.Path == "-":
		return nil, &clikit.Error{Kind: clikit.KindInvalidInput, Message: "stdin holds no event records", Hint: fromFileHint}
	case len(events) == 0:
		return nil, &clikit.Error{Kind: clikit.KindInvalidInput, Message: src.Path + " holds no event records", Hint: fromFileHint}
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
func conflictingEventsError(conflicts []merge.ImportConflict) *clikit.Error {
	header := "1 event is already stored with different content, so nothing was added:"
	if len(conflicts) != 1 {
		header = fmt.Sprintf("%d events are already stored with different content, so nothing was added:", len(conflicts))
	}
	differences := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		differences = append(differences, fmt.Sprintf("event %s differs in %s", c.ID, strings.Join(c.Fields, ", ")))
	}
	lines := append([]string{header}, clikit.FirstFew(differences, merge.ShownImportProblems)...)

	return &clikit.Error{
		Kind: clikit.KindIDConflict, Message: strings.Join(lines, "\n"),
		Hint: "Give those records new ids to add them as other events, or change the stored events with 'coroner event edit'.",
	}
}

// eventsAttachments returns the stored attachments of events.
// ponytail: one read per event; read the timeline's attachments at once if large re-runs read slowly.
func eventsAttachments(ctx context.Context, s clikit.Records, events []timeline.Event) ([]timeline.Attachment, error) {
	owners := make([]timeline.AttachmentOwner, 0, len(events))
	for _, e := range events {
		owners = append(owners, timeline.AttachmentOwner{EventID: &e.ID})
	}

	return clikit.OwnersAttachments(ctx, s, owners)
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
	Inserted  []JSON `json:"inserted"`
	Identical []JSON `json:"identical"`
}

func (a eventsAdded) Render(o clikit.Output) error {
	if o.Format == clikit.FormatJSON {
		attached := clikit.AttachmentsByOwner(a.attached, a.dbPath)
		err := o.JSON(eventsAddedJSON{
			DryRun: a.dryRun, Timelines: []export.Timeline{clikit.NewTimelineJSON(a.timeline)},
			Inserted: eventJSONs(a.inserted, attached), Identical: eventJSONs(a.identical, attached),
		})
		if err != nil {
			return err
		}
	}
	if !a.dryRun {
		for _, id := range a.ids {
			err := o.PrintID(id)
			if err != nil {
				return err
			}
		}
	}

	verb := "Added"
	if a.dryRun {
		verb = "Would add"
	}
	message := fmt.Sprintf("%s %d %s to timeline `%s`", verb, len(a.inserted), clikit.PluralNoun(len(a.inserted), "event"), a.timeline.Name)
	switch n := len(a.identical); {
	case n == 1:
		message += "; 1 was already there"
	case n > 1:
		message += fmt.Sprintf("; %d were already there", n)
	}

	return o.Notify("%s", message)
}

// eventJSONs returns events' objects, each with its attachments in attached, by event id.
func eventJSONs(events []timeline.Event, attached map[string][]clikit.AttachmentJSON) []JSON {
	objects := make([]JSON, 0, len(events))
	for _, e := range events {
		objects = append(objects, NewJSON(e, attached[e.ID]))
	}

	return objects
}
