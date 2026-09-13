package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineUseCommand(ctx context.Context, app App) *cobra.Command {
	def := timelineUseDefinition()
	cmd := &cobra.Command{
		Use:   "use {<name|id> | --clear}",
		Short: "Set the current timeline",
		Long: "Make a timeline current, so commands that act on a timeline use it when neither -t nor\n" +
			"CORONER_TIMELINE names one. Each database keeps its own current timeline. --clear leaves\n" +
			"none current.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Example: "  $ coroner timeline use mongo-outage-2026-09-11\n" +
			"  $ coroner timeline use --clear",
		RunE: writeRunE(ctx, app, def, useTimeline),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func timelineUseDefinition() definition {
	return definition{fields: []field{
		{name: "timeline", arg: true, usage: "Timeline to make current, as a `name|id`"},
		{name: "clear", typ: boolField, usage: "Leave no timeline current"},
	}}
}

// timelineUsed is the timeline use made current, nil when it cleared the current timeline.
type timelineUsed struct {
	timeline *timeline.Timeline
}

func useTimeline(ctx context.Context, app App, cfg config.Config, in fieldValues) (timelineUsed, error) {
	err := timelineUseDefinition().check(in, checkEnv{})
	if err != nil {
		return timelineUsed{}, err
	}
	nameOrID := in.value("timeline")
	clearing, _ := in.boolean("clear")
	if clearing == (nameOrID != nil) {
		return timelineUsed{}, &Error{
			Kind:    KindUsage,
			Message: "pass either a timeline name or id, or --clear",
			Hint:    "Run 'coroner timeline use --help' for usage.",
		}
	}

	var used timelineUsed
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		if clearing {
			return s.ClearCurrentTimeline(ctx)
		}

		t, err := findTimeline(ctx, s, *nameOrID)
		if err != nil {
			return err
		}
		used.timeline = &t
		return s.SetCurrentTimeline(ctx, t.ID)
	})

	return used, err
}

func (u timelineUsed) render(o output) error {
	if u.timeline == nil {
		return o.notify("No timeline is current.")
	}

	return o.notify("Now using timeline `%s`.", u.timeline.Name)
}
