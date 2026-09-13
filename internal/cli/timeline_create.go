package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineCreateCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a timeline",
		Long: "Create a timeline. Its name is how commands select it: at most 64 lowercase letters,\n" +
			"digits, '.', '_' or '-', starting with a letter or digit. Put free text in --title.\n" +
			"Creating a timeline does not make it current; run 'coroner timeline use' for that.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline create mongo-outage-2026-09-11 --title \"Primary election storm\" \\\n" +
			"      --ref INC-4821 --ref https://jira.example.com/browse/INC-4821\n" +
			"  $ coroner timeline create mongo-outage-2026-09-11 --if-not-exists --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return createTimeline(ctx, cmd, app, args[0])
		},
	}

	cmd.Flags().String("title", "", "Free-text `title`")
	cmd.Flags().StringArray("ref", nil, "Ticket key or URL the timeline belongs to; repeat for more")
	cmd.Flags().Bool("if-not-exists", false, "Print the existing timeline instead of failing when the name is taken")

	return cmd
}

func createTimeline(ctx context.Context, cmd *cobra.Command, app App, name string) error {
	flags := cmd.Flags()
	title, _ := flags.GetString("title")
	ifNotExists, _ := flags.GetBool("if-not-exists")
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}

	id, err := app.NewID()
	if err != nil {
		return err
	}
	now := app.Now()
	t := timeline.Timeline{
		ID: id.String(), Name: name, Title: title, Refs: appendUnique(nil, stringArray(flags, "ref")...),
		CreatedAt: now, UpdatedAt: now,
	}
	err = checkTimeline(t)
	if err != nil {
		return err
	}

	return withStore(ctx, cmd, app, func(s *store.Store) error {
		err := s.CreateTimeline(ctx, t)
		switch {
		case errors.Is(err, store.ErrConflict) && ifNotExists:
			existing, err := s.TimelineByName(ctx, name)
			if err != nil {
				return err
			}
			err = printTimeline(o, existing)
			if err != nil {
				return err
			}

			return o.notify("Timeline `%s` already exists.", name)
		case errors.Is(err, store.ErrConflict):
			return timelineExistsError(name, "Pick another name, or pass --if-not-exists to use the existing timeline.", err)
		case err != nil:
			return err
		}

		err = printTimeline(o, t)
		if err != nil {
			return err
		}

		return o.notify("Created timeline `%[1]s`.\nNext: coroner timeline use %[1]s\n      coroner event add -t %[1]s <text>", name)
	})
}
