package cli

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventDeleteCommand(ctx context.Context, app App) *cobra.Command {
	def := eventDeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <event>...",
		Short: "Delete events and their annotations",
		Long: "Delete events with their tags, annotations and attachments, and the blobs no other\n" +
			"attachment uses, then print each as it was, so its fields can be added again. Every event\n" +
			"is found before any is deleted: when one does not exist, nothing is deleted and delete\n" +
			"exits 3. Exit 3 means one of the events does not exist: if a retry exits 3, the first\n" +
			"attempt may already have deleted them. A deleted event's seq is never given to another\n" +
			"event.\n\n" + eventRefHelp,
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event delete 12 14 --dry-run\n" +
			"  $ coroner event delete mongo-outage-2026-09-11#12 --json",
		RunE: writeRunEInZone(ctx, app, def, deleteEvents),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func eventDeleteDefinition() definition {
	return definition{fields: []field{
		inherited(timelineField()),
		{name: "events", arg: true, typ: stringsField, usage: "Events to delete, as `ref`s: seq, timeline#seq or id", check: withoutEnv(checkEventRef)},
		{name: "dry-run", typ: boolField, usage: "Show what would be deleted, and delete nothing"},
	}}
}

// eventsDeleted are the events delete removed or, with --dry-run, would remove, with their
// attachments' objects keyed by event id, and why the blobs they left unused were not deleted.
type eventsDeleted struct {
	found    foundEvents
	attached map[string][]attachmentJSON
	dryRun   bool
	blobsErr error
}

func deleteEvents(ctx context.Context, app App, cfg config.Config, in fieldValues) (eventsDeleted, error) {
	err := eventDeleteDefinition().check(in, checkEnv{})
	if err != nil {
		return eventsDeleted{}, err
	}
	if len(in.values("events")) == 0 {
		return eventsDeleted{}, missingValue("event delete", "at least one event ref")
	}
	refs := make([]eventRef, 0, len(in.values("events")))
	for _, ref := range in.values("events") {
		parsed, err := parseEventRef(ref)
		if err != nil {
			return eventsDeleted{}, err
		}
		refs = append(refs, parsed)
	}
	dryRun, _ := in.boolean("dry-run")

	deleted := eventsDeleted{dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		found, err := findEvents(ctx, s, refs, timelineInputOf(in), app.LookupEnv)
		if err != nil {
			return err
		}
		deleted.found = found
		attachments, err := ownersAttachments(ctx, s, found.owners())
		deleted.attached = attachmentsByOwner(attachments, cfg.DBPath)
		if err != nil || dryRun {
			return err
		}

		err = s.DeleteEvents(ctx, found.ids()...)
		if errors.Is(err, store.ErrNotFound) {
			return eventNotFoundError("another coroner deleted one of the events first; nothing was deleted", err)
		}
		if err != nil {
			return err
		}
		_, deleted.blobsErr = s.GarbageCollectBlobs(ctx)

		return nil
	})

	return deleted, err
}

// render prints the events as they were, with times in z, and says what happened to them.
func (d eventsDeleted) render(o output, z zone) error {
	err := writeDeletion(o, z, d.found, d.attached)
	if err != nil {
		return err
	}
	if d.dryRun {
		return notifyDeletion(o, "Would delete", d.found)
	}

	err = notifyDeletion(o, "Deleted", d.found)
	if err != nil || d.blobsErr == nil {
		return err
	}

	return o.notify("Could not delete the blobs no attachment uses any more, so the next delete will: %v", d.blobsErr)
}

// foundEvents are events with their timelines, each once, in the order first named.
type foundEvents struct {
	events    []timeline.Event
	timelines map[string]timeline.Timeline
}

// findEvents finds the event each of refs names, failing on the first that finds none.
func findEvents(
	ctx context.Context, s records, refs []eventRef, timelines timelineInput, lookupEnv func(string) (string, bool),
) (foundEvents, error) {
	found := foundEvents{timelines: map[string]timeline.Timeline{}}
	for _, ref := range refs {
		e, t, err := findEvent(ctx, s, ref, timelines, lookupEnv)
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
func writeDeletion(o output, z zone, f foundEvents, attached map[string][]attachmentJSON) error {
	if o.format == formatJSON {
		return o.json(newEventListJSON(eventListing{
			timelines: f.timelineList(), page: store.EventPage{Events: f.events, Total: len(f.events)}, attached: attached,
		}))
	}

	summaries := make([]string, 0, len(f.events))
	for _, e := range f.events {
		summaries = append(summaries, eventSummary(e, f.timelines[e.TimelineID], z))
	}

	_, err := io.WriteString(o.out, strings.Join(summaries, "\n"))
	return err
}

// notifyDeletion says, per timeline, how many of f's events verb applies to.
func notifyDeletion(o output, verb string, f foundEvents) error {
	counts := map[string]int{}
	for _, e := range f.events {
		counts[e.TimelineID]++
	}

	for _, t := range f.timelineList() {
		noun := "events"
		if counts[t.ID] == 1 {
			noun = "event"
		}
		err := o.notify("%s %d %s from timeline `%s`.", verb, counts[t.ID], noun, t.Name)
		if err != nil {
			return err
		}
	}

	return nil
}
