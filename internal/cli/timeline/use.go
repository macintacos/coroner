package timeline

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineUseCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := UseDefinition()
	cmd := &cobra.Command{
		Use:   "use {<name|id> | --clear}",
		Short: "Set the current timeline",
		Long: "Make a timeline current, so commands that act on a timeline use it when neither -t nor\n" +
			"CORONER_TIMELINE names one. Each database keeps its own current timeline. --clear leaves\n" +
			"none current. Under --json it prints the timeline it makes current, or null with --clear.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: clikit.CompleteTimelineArg(ctx, app),
		Annotations:       map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner timeline use mongo-outage-2026-09-11\n" +
			"  $ coroner timeline use --clear",
		RunE: clikit.WriteRunE(ctx, app, def, Use),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// UseDefinition declares `timeline use`'s flags, argument and their checks.
func UseDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{Name: "timeline", Arg: true, Usage: "Timeline to make current, as a `name|id`"},
		{Name: "clear", Typ: clikit.BoolField, Usage: "Leave no timeline current"},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the timeline that would become current, and change nothing"},
	}}
}

// Used is the timeline use made, or with --dry-run would make, current; nil when it
// clears the current timeline.
type Used struct {
	timeline *timeline.Timeline
	dryRun   bool
	// warning names the CORONER_TIMELINE that still selects another timeline, or is empty.
	warning string
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (u Used) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, u.dryRun)
	if u.timeline == nil {
		logger.InfoContext(ctx, "current timeline cleared")
		return
	}
	logger.InfoContext(ctx, "current timeline set", "timeline_id", u.timeline.ID)
}

// Use runs `timeline use` from in's values and returns what it changed, or would change under --dry-run.
func Use(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Used, error) {
	err := UseDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Used{}, err
	}
	nameOrID := in.Value("timeline")
	clearing, _ := in.Boolean("clear")
	if clearing == (nameOrID != nil) {
		return Used{}, &clikit.Error{
			Kind:    clikit.KindUsage,
			Message: "pass either a timeline name or id, or --clear",
			Hint:    "Run 'coroner timeline use --help' for usage.",
		}
	}

	dryRun, _ := in.Boolean("dry-run")

	used, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Used, error) {
			used := Used{dryRun: dryRun}
			if clearing {
				return used, tx.ClearCurrentTimeline(ctx)
			}

			t, err := clikit.FindTimeline(ctx, tx, *nameOrID)
			if err != nil {
				return used, err
			}
			used.timeline = &t
			used.warning = clikit.ShadowWarning(app.LookupEnv, t)
			return used, tx.SetCurrentTimeline(ctx, t.ID)
		})

	return used, err
}

// Render prints the result in o's format.
func (u Used) Render(o clikit.Output) error {
	if o.Format == clikit.FormatJSON {
		err := o.JSON(u.object())
		if err != nil {
			return err
		}
	}

	switch {
	case u.timeline == nil && u.dryRun:
		return o.Notify("Would leave no timeline current.")
	case u.timeline == nil:
		return o.Notify("No timeline is current.")
	case u.dryRun:
		return o.Notify("Would use timeline `%s`.", u.timeline.Name)
	}

	err := o.Notify("Now using timeline `%s`.", u.timeline.Name)
	if err != nil || u.warning == "" {
		return err
	}

	return o.Notify("%s", u.warning)
}

// object returns the timeline's object, or nil, which encodes as null, for --clear.
func (u Used) object() *export.Timeline {
	if u.timeline == nil {
		return nil
	}

	return new(clikit.NewTimelineJSON(*u.timeline))
}
