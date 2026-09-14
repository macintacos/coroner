package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineDeleteCommand(ctx context.Context, app App) *cobra.Command {
	def := timelineDeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <name|id> {--yes | --dry-run}",
		Short: "Delete a timeline and everything in it",
		Long: "Delete a timeline with its events, tags, annotations, attachments and links, and the blobs\n" +
			"no other attachment uses. This cannot be undone, so it needs --yes; --dry-run shows what\n" +
			"would be deleted instead. Deleting the current timeline leaves none current, and the\n" +
			"overlays that read the timeline keep their other timelines.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Example: "  $ coroner timeline delete mongo-outage-2026-09-11 --dry-run\n" +
			"  $ coroner timeline delete mongo-outage-2026-09-11 --yes",
		RunE: writeRunE(ctx, app, def, deleteTimeline),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func timelineDeleteDefinition() definition {
	return definition{fields: []field{
		{name: "timeline", arg: true, usage: "Timeline to delete, as a `name|id`"},
		{name: "yes", typ: boolField, usage: "Delete without asking"},
		{name: "dry-run", typ: boolField, usage: "Show what would be deleted, and delete nothing"},
	}}
}

// timelineDeleted is the timeline delete removed or, with --dry-run, would remove, with the
// overlays that lose it; only --dry-run counts its events and annotations, and only a delete says
// why the blobs it left unused were not deleted.
type timelineDeleted struct {
	timeline            timeline.Timeline
	overlays            []timeline.Overlay
	dryRun              bool
	events, annotations int
	blobsErr            error
}

func deleteTimeline(ctx context.Context, app App, cfg config.Config, in fieldValues) (timelineDeleted, error) {
	err := timelineDeleteDefinition().check(in, checkEnv{})
	if err != nil {
		return timelineDeleted{}, err
	}
	nameOrID := in.value("timeline")
	if nameOrID == nil {
		return timelineDeleted{}, missingValue("timeline delete", "the timeline's name or id")
	}
	yes, _ := in.boolean("yes")
	dryRun, _ := in.boolean("dry-run")
	if !yes && !dryRun {
		return timelineDeleted{}, &Error{
			Kind:    KindUsage,
			Message: fmt.Sprintf("deleting timeline %q needs --yes", *nameOrID),
			Hint:    "Pass --yes to delete it and everything in it, or --dry-run to see what that deletes.",
		}
	}

	deleted := timelineDeleted{dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		t, err := findTimeline(ctx, s, *nameOrID)
		if err != nil {
			return err
		}
		deleted.timeline = t
		deleted.overlays, err = s.TimelineOverlays(ctx, t.ID)
		if err != nil {
			return err
		}
		if !dryRun {
			err = s.DeleteTimeline(ctx, t.ID)
			if err != nil {
				return err
			}
			_, deleted.blobsErr = s.GarbageCollectBlobs(ctx)
			return nil
		}

		events, err := s.Events(ctx, t.ID)
		if err != nil {
			return err
		}
		annotations, err := s.Annotations(ctx, t.ID)
		deleted.events, deleted.annotations = len(events), len(annotations)

		return err
	})

	return deleted, err
}

func (d timelineDeleted) render(o output) error {
	if !d.dryRun {
		err := o.notify("Deleted timeline `%s`.", d.timeline.Name)
		if err == nil {
			err = d.notifyOverlays(o, "Removed")
		}
		if err != nil || d.blobsErr == nil {
			return err
		}
		return o.notify("Could not delete the blobs no attachment uses any more, so the next delete will: %v", d.blobsErr)
	}

	_, err := fmt.Fprintf(o.out, "Would delete timeline `%s`: events: %d, annotations: %d\n", d.timeline.Name, d.events, d.annotations)
	if err != nil {
		return err
	}

	return d.notifyOverlays(o, "Would remove")
}

// notifyOverlays says, with verb, which overlays the timeline leaves; it writes nothing when there
// are none.
func (d timelineDeleted) notifyOverlays(o output, verb string) error {
	if len(d.overlays) == 0 {
		return nil
	}
	names := make([]string, 0, len(d.overlays))
	for _, overlay := range d.overlays {
		names = append(names, overlay.Name)
	}

	return o.notify("%s timeline `%s` from %s %s.", verb, d.timeline.Name, pluralNoun(len(names), "overlay"), codeList(names))
}
