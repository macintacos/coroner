package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newOverlayCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "overlay",
		Short:   "Save sets of timelines to read together",
		GroupID: coreCommands,
		Long: "An overlay is several timelines read together as one chronology. Save one under a name with\n" +
			"'coroner overlay create', then pass --overlay <name> to event list and timeline export instead\n" +
			"of repeating -t. An overlay is kept in this database only: exports never carry it.",
		Example: "  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency\n" +
			"  $ coroner event list --overlay checkout-incident",
		// Help and the unknown-verb error print under any format.
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		RunE:        runNoun,
	}

	cmd.AddCommand(newOverlayCreateCommand(ctx, app))

	return cmd
}

func newOverlayCreateCommand(ctx context.Context, app App) *cobra.Command {
	def := overlayCreateDefinition()
	cmd := &cobra.Command{
		Use:   "create <name> -t <timeline>...",
		Short: "Save a set of timelines under a name",
		Long: "Save the timelines -t names, in the order given, as an overlay called <name>: at most 64\n" +
			"lowercase letters, digits, '.', '_' or '-', starting with a letter or digit. --overlay <name>\n" +
			"then selects those timelines on event list and timeline export. Deleting a timeline removes it\n" +
			"from its overlays. --dry-run shows the overlay and saves nothing.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json", timelinesAnnotation: "several"},
		Example: "  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency\n" +
			"  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency --if-not-exists --json",
		RunE: writeRunE(ctx, app, def, createOverlay),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func overlayCreateDefinition() definition {
	timelines := timelineField()
	timelines.usage = "Timeline `name|id` the overlay reads; repeat for each, in order"

	return definition{fields: []field{
		{name: "name", arg: true, usage: "Overlay `name`, which --overlay selects it by", check: withoutEnv(checkOverlayName)},
		timelines,
		{name: "if-not-exists", typ: boolField, usage: "Print the existing overlay instead of failing when the name is taken"},
		{name: "dry-run", typ: boolField, usage: "Show the overlay as it would be saved, and save nothing"},
	}}
}

// checkOverlayName returns invalid_name, with a suggested name, unless name is a valid overlay name.
func checkOverlayName(name string) error {
	err := timeline.ValidateOverlayName(name)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_' and '-'."
	if suggestion := suggestSlug(name, namePunct, timeline.ValidateOverlayName); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`?", suggestion)
	}

	return &Error{Kind: KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// overlayCreated is the overlay create saved or, with --dry-run, would save, or the one
// --if-not-exists found, with its timelines.
type overlayCreated struct {
	overlay   timeline.Overlay
	timelines []timeline.Timeline
	existed   bool
	dryRun    bool
}

func createOverlay(ctx context.Context, app App, cfg config.Config, in fieldValues) (overlayCreated, error) {
	err := overlayCreateDefinition().check(in, checkEnv{})
	if err != nil {
		return overlayCreated{}, err
	}
	name := in.value("name")
	if name == nil {
		return overlayCreated{}, missingValue("overlay create", "the overlay's name")
	}
	timelines := timelineInputOf(in)
	if len(timelines.names) == 0 {
		return overlayCreated{}, missingValue("overlay create", "-t for each timeline it reads")
	}
	id, err := app.NewID()
	if err != nil {
		return overlayCreated{}, err
	}
	ifNotExists, _ := in.boolean("if-not-exists")
	dryRun, _ := in.boolean("dry-run")

	now := app.Now()

	var created overlayCreated
	_, err = withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(dryRun)}, func(tx *store.Tx) error {
		created = overlayCreated{dryRun: dryRun}
		sel, err := selectTimelines(ctx, tx, timelines, app.LookupEnv)
		if err != nil {
			return err
		}
		created.timelines, err = sel.all(ctx, tx)
		if err != nil {
			return err
		}
		created.overlay = timeline.Overlay{ID: id.String(), Name: *name, TimelineIDs: timelineIDs(created.timelines), CreatedAt: now}

		err = tx.CreateOverlay(ctx, created.overlay)
		switch {
		case errors.Is(err, store.ErrConflict) && ifNotExists:
			created.overlay, err = tx.OverlayByName(ctx, *name)
			if err != nil {
				return err
			}
			created.existed = true
			created.timelines, err = timelinesByID(ctx, tx, created.overlay.TimelineIDs)
			return err
		case errors.Is(err, store.ErrConflict):
			return overlayExistsError(*name, err)
		}
		return err
	})

	return created, err
}

func overlayExistsError(name string, err error) *Error {
	return &Error{
		Kind: KindOverlayExists, Message: fmt.Sprintf("overlay %q already exists", name),
		Hint: "Pick another name, or pass --if-not-exists to use the existing overlay.", Err: err,
	}
}

// timelinesByID returns the timeline with each of ids, in their order.
func timelinesByID(ctx context.Context, s records, ids []string) ([]timeline.Timeline, error) {
	timelines := make([]timeline.Timeline, 0, len(ids))
	for _, id := range ids {
		t, err := s.Timeline(ctx, id)
		if err != nil {
			return nil, err
		}
		timelines = append(timelines, t)
	}

	return timelines, nil
}

func (c overlayCreated) render(o output) error {
	if o.format == formatJSON {
		err := o.json(overlayJSON{
			ID: c.overlay.ID, Name: c.overlay.Name, Timelines: namesOf(c.timelines), CreatedAt: timeline.FormatInstant(c.overlay.CreatedAt),
		})
		if err != nil {
			return err
		}
	}
	if !c.dryRun {
		err := o.printID(c.overlay.ID)
		if err != nil {
			return err
		}
	}

	of := "of " + pluralNoun(len(c.timelines), "timeline") + " " + codeList(namesOf(c.timelines))
	switch {
	case c.existed:
		return o.notify("Overlay `%s` already exists.", c.overlay.Name)
	case c.dryRun:
		return o.notify("Would create overlay `%s` %s.", c.overlay.Name, of)
	}

	return o.notify("Created overlay `%[1]s` %[2]s.\nNext: coroner event list --overlay %[1]s", c.overlay.Name, of)
}

// overlayJSON is the CLI's overlay object's keys, every one always present.
type overlayJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Timelines are the names of the overlay's timelines, in its order.
	Timelines []string `json:"timelines"`
	CreatedAt string   `json:"created_at"`
}
