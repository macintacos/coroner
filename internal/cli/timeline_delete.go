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
			"overlays that read the timeline keep their other timelines. Under --json it prints the\n" +
			"timeline it deletes, or would.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Annotations:       map[string]string{formatsAnnotation: "text,json"},
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
// overlays that lose it and its counts of events and annotations, which only --dry-run prints.
// Only a delete reports why the blobs it left unused were not deleted.
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

	deleted, _, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(dryRun)},
		func(tx *store.Tx) (timelineDeleted, error) {
			deleted := timelineDeleted{dryRun: dryRun}
			t, err := findTimeline(ctx, tx, *nameOrID)
			if err != nil {
				return deleted, err
			}
			deleted.timeline = t
			deleted.overlays, err = tx.TimelineOverlays(ctx, t.ID)
			if err != nil {
				return deleted, err
			}
			if dryRun {
				deleted.events, deleted.annotations, err = timelineContents(ctx, tx, t.ID)
				if err != nil {
					return deleted, err
				}
			}

			return deleted, tx.DeleteTimeline(ctx, t.ID)
		})
	if err != nil {
		return deleted, err
	}
	if !dryRun {
		deleted.blobsErr = collectBlobs(ctx, app, cfg.DBPath)
	}

	return deleted, nil
}

// timelineContents returns how many events and annotations are in the timeline with id timelineID.
func timelineContents(ctx context.Context, tx *store.Tx, timelineID string) (int, int, error) {
	page, err := tx.ListEvents(ctx, store.EventFilter{TimelineIDs: []string{timelineID}, Limit: 1})
	if err != nil {
		return 0, 0, err
	}
	stored, err := tx.Annotations(ctx, timelineID)
	if err != nil {
		return 0, 0, err
	}

	return page.Total, len(stored), nil
}

func (d timelineDeleted) render(o output) error {
	err := printTimeline(o, d.timeline)
	if err != nil {
		return err
	}
	if !d.dryRun {
		err := o.notify("Deleted timeline `%s`.", d.timeline.Name)
		if err == nil {
			err = d.notifyOverlays(o, "Removed")
		}
		if err != nil {
			return err
		}
		return notifyBlobsErr(o, d.blobsErr)
	}

	summary := fmt.Sprintf("Would delete timeline `%s`: events: %d, annotations: %d", d.timeline.Name, d.events, d.annotations)
	if o.format == formatJSON {
		err = o.notify("%s", summary)
	} else {
		_, err = fmt.Fprintln(o.out, summary)
	}
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
