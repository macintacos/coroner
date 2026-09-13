package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
)

func newTimelineUseCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use {<name|id> | --clear}",
		Short: "Set the current timeline",
		Long: "Make a timeline current, so commands that act on a timeline use it when neither -t nor\n" +
			"CORONER_TIMELINE names one. Each database keeps its own current timeline. --clear leaves\n" +
			"none current.",
		Args: cobra.MaximumNArgs(1),
		Example: "  $ coroner timeline use mongo-outage-2026-09-11\n" +
			"  $ coroner timeline use --clear",
		RunE: func(cmd *cobra.Command, args []string) error {
			clearing, _ := cmd.Flags().GetBool("clear")
			switch {
			case clearing == (len(args) == 1):
				return &Error{
					Kind:    KindUsage,
					Message: "pass either a timeline name or id, or --clear",
					Hint:    "Run 'coroner timeline use --help' for usage.",
				}
			case clearing:
				return clearTimeline(ctx, cmd, app)
			}

			return useTimeline(ctx, cmd, app, args[0])
		},
	}

	cmd.Flags().Bool("clear", false, "Leave no timeline current")

	return cmd
}

func useTimeline(ctx context.Context, cmd *cobra.Command, app App, nameOrID string) error {
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}

	return withStore(ctx, cmd, app, func(s *store.Store) error {
		t, err := findTimeline(ctx, s, nameOrID)
		if err != nil {
			return err
		}
		err = s.SetCurrentTimeline(ctx, t.ID)
		if err != nil {
			return err
		}

		return o.notify("Now using timeline `%s`.", t.Name)
	})
}

func clearTimeline(ctx context.Context, cmd *cobra.Command, app App) error {
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}

	return withStore(ctx, cmd, app, func(s *store.Store) error {
		err := s.ClearCurrentTimeline(ctx)
		if err != nil {
			return err
		}

		return o.notify("No timeline is current.")
	})
}
