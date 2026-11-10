package timeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineDeleteCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := DeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <name|id> {--yes | --dry-run}",
		Short: "Delete a timeline and everything in it",
		Long: "Delete a timeline with its events, tags, annotations, attachments and links, and the blobs\n" +
			"no other attachment uses. This cannot be undone, so it needs --yes; --dry-run shows what\n" +
			"would be deleted instead. Deleting the current timeline leaves none current, and the\n" +
			"overlays that read the timeline keep their other timelines. Under --json it prints the\n" +
			"timeline it deletes, or would.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: clikit.CompleteTimelineArg(ctx, app),
		Annotations:       map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner timeline delete mongo-outage-2026-09-11 --dry-run\n" +
			"  $ coroner timeline delete mongo-outage-2026-09-11 --yes",
		RunE: clikit.WriteRunE(ctx, app, def, Delete),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// DeleteDefinition declares `timeline delete`'s flags, argument and their checks.
func DeleteDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{Name: "timeline", Arg: true, Usage: "Timeline to delete, as a `name|id`"},
		{Name: "yes", Typ: clikit.BoolField, Usage: "Delete without asking"},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show what would be deleted, and delete nothing"},
	}}
}

// Deleted is the timeline delete removed or, with --dry-run, would remove, with the
// overlays that lose it and what it sweeps along, whose counts of events and annotations only
// --dry-run prints. Only a delete reports why the blobs it left unused were not deleted.
type Deleted struct {
	timeline timeline.Timeline
	overlays []timeline.Overlay
	dryRun   bool
	swept    clikit.Sweep
	blobsErr error
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (d Deleted) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, d.dryRun)
	logger.InfoContext(ctx, "timeline deleted", "timeline_id", d.timeline.ID)
	d.swept.Log(ctx, logger)
}

// timelineSweep returns what deleting the timeline with id timelineID removes beside it, with
// overlays, the overlays it leaves.
func timelineSweep(ctx context.Context, tx *store.Tx, timelineID string, overlays []timeline.Overlay) (clikit.Sweep, error) {
	events, err := tx.Events(ctx, timelineID)
	if err != nil {
		return clikit.Sweep{}, err
	}
	annotations, err := tx.Annotations(ctx, timelineID)
	if err != nil {
		return clikit.Sweep{}, err
	}
	attachments, err := tx.TimelineAttachments(ctx, timelineID)
	if err != nil {
		return clikit.Sweep{}, err
	}
	eventIDs := clikit.IDsOf(events, clikit.EventID)
	links, err := clikit.EventLinkIDs(ctx, tx, eventIDs)
	if err != nil {
		return clikit.Sweep{}, err
	}

	return clikit.Sweep{
		Events:      eventIDs,
		Annotations: clikit.IDsOf(annotations, clikit.AnnotationID),
		Attachments: clikit.IDsOf(attachments, clikit.AttachmentID),
		Links:       links,
		Overlays:    clikit.IDsOf(overlays, clikit.OverlayID),
	}, nil
}

// Delete runs `timeline delete` from in's values and returns what it changed, or would change under --dry-run.
func Delete(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Deleted, error) {
	err := DeleteDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Deleted{}, err
	}
	nameOrID := in.Value("timeline")
	if nameOrID == nil {
		return Deleted{}, clikit.MissingValue("timeline delete", "the timeline's name or id")
	}
	yes, _ := in.Boolean("yes")
	dryRun, _ := in.Boolean("dry-run")
	if !yes && !dryRun {
		return Deleted{}, &clikit.Error{
			Kind:    clikit.KindUsage,
			Message: fmt.Sprintf("deleting timeline %q needs --yes", *nameOrID),
			Hint:    "Pass --yes to delete it and everything in it, or --dry-run to see what that deletes.",
		}
	}

	deleted, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Deleted, error) {
			deleted := Deleted{dryRun: dryRun}
			t, err := clikit.FindTimeline(ctx, tx, *nameOrID)
			if err != nil {
				return deleted, err
			}
			deleted.timeline = t
			deleted.overlays, err = tx.TimelineOverlays(ctx, t.ID)
			if err != nil {
				return deleted, err
			}
			deleted.swept, err = timelineSweep(ctx, tx, t.ID, deleted.overlays)
			if err != nil {
				return deleted, err
			}

			return deleted, tx.DeleteTimeline(ctx, t.ID)
		})
	if err != nil {
		return deleted, err
	}
	if !dryRun {
		deleted.blobsErr = clikit.CollectBlobs(ctx, app, cfg.DBPath)
	}

	return deleted, nil
}

// Render prints the result in o's format.
func (d Deleted) Render(o clikit.Output) error {
	err := printTimeline(o, d.timeline)
	if err != nil {
		return err
	}
	if !d.dryRun {
		err := o.Notify("Deleted timeline `%s`.", d.timeline.Name)
		if err == nil {
			err = d.notifyOverlays(o, "Removed")
		}
		if err != nil {
			return err
		}
		return clikit.NotifyBlobsErr(o, d.blobsErr)
	}

	summary := fmt.Sprintf("Would delete timeline `%s`: events: %d, annotations: %d", d.timeline.Name, len(d.swept.Events), len(d.swept.Annotations))
	if o.Format == clikit.FormatJSON {
		err = o.Notify("%s", summary)
	} else {
		_, err = fmt.Fprintln(o.Out, summary)
	}
	if err != nil {
		return err
	}

	return d.notifyOverlays(o, "Would remove")
}

// notifyOverlays says, with verb, which overlays the timeline leaves; it writes nothing when there
// are none.
func (d Deleted) notifyOverlays(o clikit.Output, verb string) error {
	if len(d.overlays) == 0 {
		return nil
	}
	names := make([]string, 0, len(d.overlays))
	for _, overlay := range d.overlays {
		names = append(names, overlay.Name)
	}

	return o.Notify("%s timeline `%s` from %s %s.", verb, d.timeline.Name, clikit.PluralNoun(len(names), "overlay"), clikit.CodeList(names))
}
