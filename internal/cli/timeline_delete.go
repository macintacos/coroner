package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineDeleteCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name|id> {--yes | --dry-run}",
		Short: "Delete a timeline and everything in it",
		Long: "Delete a timeline with its events, tags and annotations. This cannot be undone, so it\n" +
			"needs --yes; --dry-run shows what would be deleted instead. Deleting the current\n" +
			"timeline leaves none current.",
		Args: cobra.ExactArgs(1),
		Example: "  $ coroner timeline delete mongo-outage-2026-09-11 --dry-run\n" +
			"  $ coroner timeline delete mongo-outage-2026-09-11 --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			yes, _ := cmd.Flags().GetBool("yes")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if !yes && !dryRun {
				return &Error{
					Kind:    KindUsage,
					Message: fmt.Sprintf("deleting timeline %q needs --yes", args[0]),
					Hint:    "Pass --yes to delete it and everything in it, or --dry-run to see what that deletes.",
				}
			}

			return withStore(ctx, cmd, app, func(s *store.Store) error {
				t, err := findTimeline(ctx, s, args[0])
				if err != nil {
					return err
				}
				if dryRun {
					return reportDeletion(ctx, s, app.Out, t)
				}

				err = s.DeleteTimeline(ctx, t.ID)
				if err != nil {
					return err
				}

				return notify(cmd, app, fmt.Sprintf("Deleted timeline `%s`.\n", t.Name))
			})
		},
	}

	cmd.Flags().Bool("yes", false, "Delete without asking")
	cmd.Flags().Bool("dry-run", false, "Show what would be deleted, and delete nothing")

	return cmd
}

// reportDeletion writes to w what deleting t would remove.
func reportDeletion(ctx context.Context, s *store.Store, w io.Writer, t timeline.Timeline) error {
	events, err := s.Events(ctx, t.ID)
	if err != nil {
		return err
	}
	annotations, err := s.Annotations(ctx, t.ID)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "Would delete timeline `%s`: %d events, %d annotations.\n", t.Name, len(events), len(annotations))
	return err
}
