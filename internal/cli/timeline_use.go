package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
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
			"none current. Under --json it prints the timeline it makes current, or null with --clear.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Annotations:       map[string]string{formatsAnnotation: "text,json"},
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
		{name: "dry-run", typ: boolField, usage: "Show the timeline that would become current, and change nothing"},
	}}
}

// timelineUsed is the timeline use made, or with --dry-run would make, current; nil when it
// clears the current timeline.
type timelineUsed struct {
	timeline *timeline.Timeline
	dryRun   bool
	// warning names the CORONER_TIMELINE that still selects another timeline, or is empty.
	warning string
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

	dryRun, _ := in.boolean("dry-run")

	used, _, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(dryRun)},
		func(tx *store.Tx) (timelineUsed, error) {
			used := timelineUsed{dryRun: dryRun}
			if clearing {
				return used, tx.ClearCurrentTimeline(ctx)
			}

			t, err := findTimeline(ctx, tx, *nameOrID)
			if err != nil {
				return used, err
			}
			used.timeline = &t
			used.warning = shadowWarning(app.LookupEnv, t)
			return used, tx.SetCurrentTimeline(ctx, t.ID)
		})

	return used, err
}

func (u timelineUsed) render(o output) error {
	if o.format == formatJSON {
		err := o.json(u.object())
		if err != nil {
			return err
		}
	}

	switch {
	case u.timeline == nil && u.dryRun:
		return o.notify("Would leave no timeline current.")
	case u.timeline == nil:
		return o.notify("No timeline is current.")
	case u.dryRun:
		return o.notify("Would use timeline `%s`.", u.timeline.Name)
	}

	err := o.notify("Now using timeline `%s`.", u.timeline.Name)
	if err != nil || u.warning == "" {
		return err
	}

	return o.notify("%s", u.warning)
}

// object returns the timeline's object, or nil, which encodes as null, for --clear.
func (u timelineUsed) object() *export.Timeline {
	if u.timeline == nil {
		return nil
	}

	return new(newTimelineJSON(*u.timeline))
}
