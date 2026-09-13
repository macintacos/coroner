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
		Long: "Delete a timeline with its events, tags and annotations. This cannot be undone, so it\n" +
			"needs --yes; --dry-run shows what would be deleted instead. Deleting the current\n" +
			"timeline leaves none current.",
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

// timelineDeleted is the timeline delete removed or, with --dry-run, would remove; only --dry-run
// counts its events and annotations.
type timelineDeleted struct {
	timeline            timeline.Timeline
	dryRun              bool
	events, annotations int
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
		if !dryRun {
			return s.DeleteTimeline(ctx, t.ID)
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
		return o.notify("Deleted timeline `%s`.", d.timeline.Name)
	}

	_, err := fmt.Fprintf(o.out, "Would delete timeline `%s`: events: %d, annotations: %d\n", d.timeline.Name, d.events, d.annotations)
	return err
}
