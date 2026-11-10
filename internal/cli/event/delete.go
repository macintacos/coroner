package event

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventDeleteCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := DeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <event>...",
		Short: "Delete events and their annotations",
		Long: "Delete events with their tags, annotations and attachments, and the blobs no other\n" +
			"attachment uses, then print each as it was, so its fields can be added again. Every event\n" +
			"is found before any is deleted: when one does not exist, nothing is deleted and delete\n" +
			"exits 3. Exit 3 means one of the events does not exist: if a retry exits 3, the first\n" +
			"attempt may already have deleted them. A deleted event's seq is never given to another\n" +
			"event.\n\n" + clikit.EventRefHelp,
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner event delete 12 14 --dry-run\n" +
			"  $ coroner event delete mongo-outage-2026-09-11#12 --json",
		RunE: clikit.WriteRunEInZone(ctx, app, def, Delete),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// DeleteDefinition declares `event delete`'s flags, argument and their checks.
func DeleteDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		clikit.Inherited(clikit.TimelineField()),
		{Name: "events", Arg: true, Typ: clikit.StringsField, Usage: "Events to delete, as `ref`s: seq, timeline#seq or id", Check: clikit.WithoutEnv(clikit.CheckEventRef)},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show what would be deleted, and delete nothing"},
	}}
}

// Deleted are the events delete removed or, with --dry-run, would remove, with their
// attachments' objects keyed by event id, what they sweep along, and why the blobs they left
// unused were not deleted.
type Deleted struct {
	found    foundEvents
	attached map[string][]clikit.AttachmentJSON
	dryRun   bool
	swept    clikit.Sweep
	blobsErr error
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (d Deleted) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, d.dryRun)
	for _, e := range d.found.events {
		logger.InfoContext(ctx, "event deleted", "event", e.ID, "timeline_id", e.TimelineID)
	}
	d.swept.Log(ctx, logger)
}

// eventsSweep returns what deleting events removes beside them: their annotations; attachments,
// the events' own then their annotations'; and their links.
func eventsSweep(ctx context.Context, tx *store.Tx, found foundEvents, eventAttachments []timeline.Attachment) (clikit.Sweep, error) {
	var annotations []timeline.Annotation
	for _, e := range found.events {
		eventAnnotations, err := tx.EventAnnotations(ctx, e.ID)
		if err != nil {
			return clikit.Sweep{}, err
		}
		annotations = append(annotations, eventAnnotations...)
	}
	owners := make([]timeline.AttachmentOwner, 0, len(annotations))
	for _, a := range annotations {
		owners = append(owners, timeline.AttachmentOwner{AnnotationID: &a.ID})
	}
	annotationAttachments, err := clikit.OwnersAttachments(ctx, tx, owners)
	if err != nil {
		return clikit.Sweep{}, err
	}
	links, err := clikit.EventLinkIDs(ctx, tx, found.ids())
	if err != nil {
		return clikit.Sweep{}, err
	}

	return clikit.Sweep{
		Annotations: clikit.IDsOf(annotations, clikit.AnnotationID),
		Attachments: clikit.IDsOf(slices.Concat(eventAttachments, annotationAttachments), clikit.AttachmentID),
		Links:       links,
	}, nil
}

// Delete runs `event delete` from in's values and returns what it changed, or would change under --dry-run.
func Delete(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Deleted, error) {
	err := DeleteDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Deleted{}, err
	}
	if len(in.Values("events")) == 0 {
		return Deleted{}, clikit.MissingValue("event delete", "at least one event ref")
	}
	refs := make([]clikit.EventRef, 0, len(in.Values("events")))
	for _, ref := range in.Values("events") {
		parsed, err := clikit.ParseEventRef(ref)
		if err != nil {
			return Deleted{}, err
		}
		refs = append(refs, parsed)
	}
	dryRun, _ := in.Boolean("dry-run")

	deleted, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Deleted, error) {
			deleted := Deleted{dryRun: dryRun}
			found, err := findEvents(ctx, app.Logger(), tx, refs, clikit.TimelineInputOf(in), app.LookupEnv)
			if err != nil {
				return deleted, err
			}
			deleted.found = found
			attachments, err := clikit.OwnersAttachments(ctx, tx, found.owners())
			deleted.attached = clikit.AttachmentsByOwner(attachments, cfg.DBPath)
			if err != nil {
				return deleted, err
			}
			deleted.swept, err = eventsSweep(ctx, tx, found, attachments)
			if err != nil {
				return deleted, err
			}

			return deleted, tx.DeleteEvents(ctx, found.ids()...)
		})
	if err != nil {
		return deleted, err
	}
	if !dryRun {
		deleted.blobsErr = clikit.CollectBlobs(ctx, app, cfg.DBPath)
	}

	return deleted, nil
}

// Render prints the events as they were, with times in z, and says what happened to them.
func (d Deleted) Render(o clikit.Output, z clikit.Zone) error {
	err := writeDeletion(o, z, d.found, d.attached)
	if err != nil {
		return err
	}
	if d.dryRun {
		return notifyDeletion(o, "Would delete", d.found)
	}

	err = notifyDeletion(o, "Deleted", d.found)
	if err != nil {
		return err
	}

	return clikit.NotifyBlobsErr(o, d.blobsErr)
}

// foundEvents are events with their timelines, each once, in the order first named.
type foundEvents struct {
	events    []timeline.Event
	timelines map[string]timeline.Timeline
}

// findEvents finds the event each of refs names, failing on the first that finds none.
func findEvents(
	ctx context.Context, logger *slog.Logger, s clikit.Records, refs []clikit.EventRef, timelines clikit.TimelineInput, lookupEnv func(string) (string, bool),
) (foundEvents, error) {
	found := foundEvents{timelines: map[string]timeline.Timeline{}}
	for _, ref := range refs {
		e, t, err := clikit.FindEvent(ctx, logger, s, ref, timelines, lookupEnv)
		if err != nil {
			return found, err
		}
		if !slices.ContainsFunc(found.events, func(existing timeline.Event) bool { return existing.ID == e.ID }) {
			found.events = append(found.events, e)
			found.timelines[t.ID] = t
		}
	}

	return found, nil
}

func (f foundEvents) ids() []string {
	ids := make([]string, 0, len(f.events))
	for _, e := range f.events {
		ids = append(ids, e.ID)
	}

	return ids
}

// timelineList returns f's timelines, each once, in the order their events were first named.
func (f foundEvents) timelineList() []timeline.Timeline {
	var timelines []timeline.Timeline
	for _, e := range f.events {
		t := f.timelines[e.TimelineID]
		if !slices.ContainsFunc(timelines, func(listed timeline.Timeline) bool { return listed.ID == t.ID }) {
			timelines = append(timelines, t)
		}
	}

	return timelines
}

func (f foundEvents) owners() []timeline.AttachmentOwner {
	owners := make([]timeline.AttachmentOwner, 0, len(f.events))
	for _, e := range f.events {
		owners = append(owners, timeline.AttachmentOwner{EventID: &e.ID})
	}

	return owners
}

// writeDeletion prints the events f holds as they were: the event list's JSON, with attachments
// from attached keyed by event id, or a summary of each.
func writeDeletion(o clikit.Output, z clikit.Zone, f foundEvents, attached map[string][]clikit.AttachmentJSON) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(newEventListJSON(eventListing{
			timelines: f.timelineList(), page: store.EventPage{Events: f.events, Total: len(f.events)}, attached: attached,
		}))
	}

	summaries := make([]string, 0, len(f.events))
	for _, e := range f.events {
		summaries = append(summaries, eventSummary(e, f.timelines[e.TimelineID], z))
	}

	_, err := io.WriteString(o.Out, strings.Join(summaries, "\n"))
	return err
}

// notifyDeletion says, per timeline, how many of f's events verb applies to.
func notifyDeletion(o clikit.Output, verb string, f foundEvents) error {
	counts := map[string]int{}
	for _, e := range f.events {
		counts[e.TimelineID]++
	}

	for _, t := range f.timelineList() {
		noun := "events"
		if counts[t.ID] == 1 {
			noun = "event"
		}
		err := o.Notify("%s %d %s from timeline `%s`.", verb, counts[t.ID], noun, t.Name)
		if err != nil {
			return err
		}
	}

	return nil
}
