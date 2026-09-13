package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
)

func newTimelineUseCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use {<name|id> | --clear}",
		Short: "Set the current timeline",
		Long: "Make a timeline current, so commands that act on a timeline use it when none is named.\n" +
			"Each database keeps its own current timeline. --clear leaves none current.",
		Args: cobra.MaximumNArgs(1),
		Example: "  $ coroner timeline use mongo-outage-2026-09-11\n" +
			"  $ coroner timeline use --clear",
		RunE: func(cmd *cobra.Command, args []string) error {
			clearing, _ := cmd.Flags().GetBool("clear")
			if clearing == (len(args) == 1) {
				return &Error{
					Kind:    KindUsage,
					Message: "pass either a timeline name or id, or --clear",
					Hint:    "Run 'coroner timeline use --help' for usage.",
				}
			}

			return withStore(ctx, cmd, app, func(s *store.Store) error {
				message, err := useTimeline(ctx, s, args)
				if err != nil {
					return err
				}

				return notify(cmd, app, message)
			})
		},
	}

	cmd.Flags().Bool("clear", false, "Leave no timeline current")

	return cmd
}

// useTimeline makes the timeline args[0] names current, or clears the current timeline when
// args is empty, and returns the message saying which.
func useTimeline(ctx context.Context, s *store.Store, args []string) (string, error) {
	if len(args) == 0 {
		return "No timeline is current.\n", s.ClearCurrentTimeline(ctx)
	}

	t, err := findTimeline(ctx, s, args[0])
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Now using timeline `%s`.\n", t.Name), s.SetCurrentTimeline(ctx, t.ID)
}
