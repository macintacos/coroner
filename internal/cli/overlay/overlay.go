// Package overlay holds the `coroner overlay` commands.
package overlay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// NewCommand returns the overlay command and its verbs, which run against app.
func NewCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "overlay",
		Short:   "Save sets of timelines to read together",
		GroupID: clikit.CoreCommands,
		Long: "An overlay is several timelines read together as one chronology. Save one under a name with\n" +
			"'coroner overlay create', then pass --overlay <name> to event list and timeline export instead\n" +
			"of repeating -t. An overlay is kept in this database only: exports never carry it.",
		Example: "  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency\n" +
			"  $ coroner event list --overlay checkout-incident",
		RunE: clikit.RunNoun,
	}

	cmd.AddCommand(newOverlayCreateCommand(ctx, app))

	return cmd
}

func newOverlayCreateCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := CreateDefinition()
	cmd := &cobra.Command{
		Use:   "create <name> -t <timeline>...",
		Short: "Save a set of timelines under a name",
		Long: "Save the timelines -t names, in the order given, as an overlay called <name>: at most 64\n" +
			"lowercase letters, digits, '.', '_' or '-', starting with a letter or digit. --overlay <name>\n" +
			"then selects those timelines on event list and timeline export. Deleting a timeline removes it\n" +
			"from its overlays. --dry-run shows the overlay and saves nothing.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json", clikit.TimelinesAnnotation: "several"},
		Example: "  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency\n" +
			"  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency --if-not-exists --json",
		RunE: clikit.WriteRunE(ctx, app, def, Create),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// CreateDefinition declares `overlay create`'s flags, argument and their checks.
func CreateDefinition() clikit.Definition {
	timelines := clikit.TimelineField()
	timelines.Usage = "Timeline `name|id` the overlay reads; repeat for each, in order"

	return clikit.Definition{Fields: []clikit.Field{
		{Name: "name", Arg: true, Usage: "Overlay `name`, which --overlay selects it by", Check: clikit.WithoutEnv(checkOverlayName)},
		timelines,
		{Name: "if-not-exists", Typ: clikit.BoolField, Usage: "Print the existing overlay instead of failing when the name is taken"},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the overlay as it would be saved, and save nothing"},
	}}
}

// checkOverlayName returns invalid_name, with a suggested name, unless name is a valid overlay name.
func checkOverlayName(name string) error {
	err := timeline.ValidateOverlayName(name)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_' and '-'."
	if suggestion := timeline.SuggestOverlayName(name); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`?", suggestion)
	}

	return &clikit.Error{Kind: clikit.KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// Created is the overlay create saved or, with --dry-run, would save, or the one
// --if-not-exists found, with its timelines.
type Created struct {
	overlay   timeline.Overlay
	timelines []timeline.Timeline
	existed   bool
	dryRun    bool
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (c Created) LogChanges(ctx context.Context, logger *slog.Logger) {
	if c.existed {
		return
	}
	clikit.WithDryRun(logger, c.dryRun).InfoContext(ctx, "overlay created", "overlay_id", c.overlay.ID)
}

// Create runs `overlay create` from in's values and returns what it changed, or would change under --dry-run.
func Create(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Created, error) {
	err := CreateDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Created{}, err
	}
	name := in.Value("name")
	if name == nil {
		return Created{}, clikit.MissingValue("overlay create", "the overlay's name")
	}
	timelines := clikit.TimelineInputOf(in)
	if len(timelines.Names) == 0 {
		return Created{}, clikit.MissingValue("overlay create", "-t for each timeline it reads")
	}
	id, err := app.NewID()
	if err != nil {
		return Created{}, err
	}
	ifNotExists, _ := in.Boolean("if-not-exists")
	dryRun, _ := in.Boolean("dry-run")

	now := app.Now()

	created, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Created, error) {
			created := Created{dryRun: dryRun}
			sel, err := clikit.SelectTimelines(ctx, app.Logger(), tx, timelines, app.LookupEnv)
			if err != nil {
				return created, err
			}
			created.timelines, err = sel.All(ctx, tx)
			if err != nil {
				return created, err
			}
			created.overlay = timeline.Overlay{ID: id.String(), Name: *name, TimelineIDs: clikit.TimelineIDs(created.timelines), CreatedAt: now}

			err = tx.CreateOverlay(ctx, created.overlay)
			switch {
			case errors.Is(err, store.ErrConflict) && ifNotExists:
				created.overlay, err = tx.OverlayByName(ctx, *name)
				if err != nil {
					return created, err
				}
				created.existed = true
				created.timelines, err = clikit.TimelinesByID(ctx, tx, created.overlay.TimelineIDs)
				return created, err
			case errors.Is(err, store.ErrConflict):
				return created, overlayExistsError(*name, err)
			}
			return created, err
		})

	return created, err
}

func overlayExistsError(name string, err error) *clikit.Error {
	return &clikit.Error{
		Kind: clikit.KindOverlayExists, Message: fmt.Sprintf("overlay %q already exists", name),
		Hint: "Pick another name, or pass --if-not-exists to use the existing overlay.", Err: err,
	}
}

// Render prints the result in o's format.
func (c Created) Render(o clikit.Output) error {
	if o.Format == clikit.FormatJSON {
		err := o.JSON(overlayJSON{
			ID: c.overlay.ID, Name: c.overlay.Name, Timelines: clikit.NamesOf(c.timelines), CreatedAt: timeline.FormatInstant(c.overlay.CreatedAt),
		})
		if err != nil {
			return err
		}
	}
	if !c.dryRun {
		err := o.PrintID(c.overlay.ID)
		if err != nil {
			return err
		}
	}

	of := "of " + clikit.PluralNoun(len(c.timelines), "timeline") + " " + clikit.CodeList(clikit.NamesOf(c.timelines))
	switch {
	case c.existed:
		return o.Notify("Overlay `%s` already exists.", c.overlay.Name)
	case c.dryRun:
		return o.Notify("Would create overlay `%s` %s.", c.overlay.Name, of)
	}

	return o.Notify("Created overlay `%[1]s` %[2]s.\nNext: coroner event list --overlay %[1]s", c.overlay.Name, of)
}

// overlayJSON is the CLI's overlay object's keys, every one always present.
type overlayJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Timelines are the names of the overlay's timelines, in its order.
	Timelines []string `json:"timelines"`
	CreatedAt string   `json:"created_at"`
}
