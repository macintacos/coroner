package timeline

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineCreateCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a timeline",
		Long: "Create a timeline. Its name is how commands select it: at most 64 lowercase letters,\n" +
			"digits, '.', '_' or '-', starting with a letter or digit. Put free text in --title.\n" +
			"Creating a timeline does not make it current; run 'coroner timeline use' for that.\n\n" +
			"On a terminal, with no name or with --interactive, a form asks for the name, title and\n" +
			"refs, offers the existing timeline when the name is taken, and prints the command it equals.\n" +
			"Under --json only --interactive opens it, and --no-input never does.",
		Args:        oneArgOrNone,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner timeline create mongo-outage-2026-09-11 --title \"Primary election storm\" \\\n" +
			"      --ref INC-4821 --ref https://jira.example.com/browse/INC-4821\n" +
			"  $ coroner timeline create mongo-outage-2026-09-11 --if-not-exists --json\n" +
			"  $ coroner timeline create --interactive",
	}

	return CreateForm(ctx, app).Attach(cmd, app, clikit.TerminalForms(app, timelineCreateTitle))
}

const timelineCreateTitle = "📒 New timeline"

// CreateForm is timeline create with its form, which opens on a terminal when the name is
// missing.
func CreateForm(ctx context.Context, app clikit.App) clikit.FormCommand {
	create := timelineCreateWrite()
	return clikit.FormCommand{
		Def:       create.Def,
		Ready:     func(in clikit.FieldValues) bool { return clikit.RequiredGiven(create.Def, in) },
		FlagsOnly: withOneArg(clikit.WriteRun(ctx, app, create.Def, create.Run)),
		Session: func(s clikit.FormSession, in clikit.FieldValues) error {
			_, _, err := askTimelineCreate(ctx, s, create, in)
			return err
		},
	}
}

func timelineCreateWrite() clikit.FormWrite[Created] {
	return clikit.FormWrite[Created]{Path: "timeline create", Def: CreateDefinition(), Run: Create}
}

// askTimelineCreate asks for the timeline to create, prefilled from in, until one is created or
// used, or Esc ends the form. A taken name offers the existing timeline, or uses it when in gives
// --if-not-exists. It returns that timeline, and whether there is one: none after Esc, or after a
// dry run that created nothing.
func askTimelineCreate(
	ctx context.Context, s clikit.FormSession, create clikit.FormWrite[Created], in clikit.FieldValues,
) (timeline.Timeline, bool, error) {
	taken, err := timelineNames(ctx, s)
	if err != nil {
		return timeline.Timeline{}, false, err
	}
	ifNotExists, _ := in.Boolean("if-not-exists")
	note := func(name string) string { return "Timeline `" + name + "` exists; submitting offers to use it." }
	if ifNotExists {
		note = func(name string) string { return "Timeline `" + name + "` exists; submitting uses it." }
	}
	def := clikit.WithoutField(create.Def, "if-not-exists")
	def.Fields[def.FieldIndex("name")].Preview = NamePreview(taken, note)

	var made Created
	_, _, err = s.AskUntil(ctx, def, clikit.FormChoices{}, in, func(typed clikit.FieldValues) error {
		if ifNotExists {
			typed["if-not-exists"] = []string{"true"}
		}
		result, err := create.Submit(ctx, s, typed)
		rejected, _ := errors.AsType[*clikit.Error](err)
		if rejected != nil && rejected.Kind == clikit.KindTimelineExists {
			result, err = useExisting(ctx, s, create, typed)
		}
		if err == nil {
			made = result
		}
		return err
	})

	return made.timeline, made.stored(), err
}

// useExisting asks whether to use the existing timeline typed names, and prints it as
// --if-not-exists does when so, returning what that prints. Picking another name returns
// ErrAskAgain; Esc writes nothing.
func useExisting(ctx context.Context, s clikit.FormSession, create clikit.FormWrite[Created], typed clikit.FieldValues) (Created, error) {
	use := true
	confirm := huh.NewConfirm().Title("Timeline `" + *typed.Value("name") + "` already exists").
		Affirmative("Use it").Negative("Pick another name").Value(&use)
	submitted, err := s.Ask(ctx, s.Frame, clikit.FormGroup{Fields: []huh.Field{confirm}})
	switch {
	case err != nil:
		return Created{}, clikit.AskError(err)
	case !submitted:
		return Created{}, nil
	}
	// Value can rebind the confirm, so read what it holds.
	if use, _ = confirm.GetValue().(bool); !use {
		return Created{}, clikit.ErrAskAgain
	}

	typed["if-not-exists"] = []string{"true"}
	return create.Submit(ctx, s, typed)
}

// timelineNames returns the names of the timelines in the session's database.
func timelineNames(ctx context.Context, s clikit.FormSession) ([]string, error) {
	var names []string
	err := clikit.WithStorePath(ctx, s.App, s.Inv.Cfg.DBPath, func(st *store.Store) error {
		timelines, err := st.Timelines(ctx)
		for _, t := range timelines {
			names = append(names, t.Name)
		}
		return err
	})

	return names, err
}

// NamePreview returns the preview of a timeline name field: CheckName's hint for a rejected name,
// note for one of taken, and nothing otherwise.
func NamePreview(taken []string, note func(name string) string) func(string, clikit.CheckEnv) string {
	return func(name string, _ clikit.CheckEnv) string {
		rejected, ok := errors.AsType[*clikit.Error](CheckName(name))
		switch {
		case name == "":
			return ""
		case ok:
			return rejected.Hint
		case slices.Contains(taken, name):
			return note(name)
		}

		return ""
	}
}

// CreateDefinition declares `timeline create`'s flags, argument and their checks.
func CreateDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{
			Name: "name", Usage: "Timeline `name`, which commands select it by", Arg: true, Required: true,
			Check: clikit.WithoutEnv(CheckName), Suggest: SuggestName,
		},
		{Name: "title", Usage: "Free-text `title`", Check: clikit.Utf8Only("--title")},
		{
			Name: "ref", Typ: clikit.StringsField, List: true, Usage: "Ticket key or URL the timeline belongs to; repeat for more",
			Check: clikit.Utf8Only("--ref"),
		},
		{Name: "if-not-exists", Typ: clikit.BoolField, Usage: "Print the existing timeline instead of failing when the name is taken"},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the timeline as it would be created, and create nothing"},
	}}
}

// Created is the timeline create made or, with --dry-run, would make, or the one
// --if-not-exists found.
type Created struct {
	timeline        timeline.Timeline
	existed, dryRun bool
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (c Created) LogChanges(ctx context.Context, logger *slog.Logger) {
	if c.existed {
		return
	}
	clikit.WithDryRun(logger, c.dryRun).InfoContext(ctx, "timeline created", "timeline_id", c.timeline.ID)
}

// Create runs `timeline create` from in's values and returns what it changed, or would change under --dry-run.
func Create(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Created, error) {
	// Before the checks, so a rejected name still uses an ID, as the contract scripts number them.
	id, err := app.NewID()
	if err != nil {
		return Created{}, err
	}
	err = CreateDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Created{}, err
	}
	name := in.Value("name")
	if name == nil {
		return Created{}, clikit.MissingValue("timeline create", "the timeline's name")
	}
	now := app.Now()
	t := timeline.Timeline{
		ID: id.String(), Name: *name, Refs: clikit.AppendUnique(nil, in.Values("ref")...),
		CreatedAt: now, UpdatedAt: now,
	}
	if title := in.Value("title"); title != nil {
		t.Title = *title
	}
	err = checkTimeline(t)
	if err != nil {
		return Created{}, err
	}
	ifNotExists, _ := in.Boolean("if-not-exists")
	dryRun, _ := in.Boolean("dry-run")

	created, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Created, error) {
			created := Created{timeline: t, dryRun: dryRun}
			err := tx.CreateTimeline(ctx, t)
			switch {
			case errors.Is(err, store.ErrConflict) && ifNotExists:
				created.timeline, err = tx.TimelineByName(ctx, t.Name)
				created.existed = true
				return created, err
			case errors.Is(err, store.ErrConflict):
				return created, timelineExistsError(t.Name, "Pick another name, or pass --if-not-exists to use the existing timeline.", err)
			}

			return created, err
		})

	return created, err
}

// stored reports whether c's timeline is in the database: created, or found by --if-not-exists.
func (c Created) stored() bool {
	return c.existed || c.timeline.ID != "" && !c.dryRun
}

// Render prints the result in o's format.
func (c Created) Render(o clikit.Output) error {
	err := printTimeline(o, c.timeline)
	if err == nil && !c.dryRun {
		err = o.PrintID(c.timeline.ID)
	}
	if err != nil {
		return err
	}
	switch {
	case c.existed:
		return o.Notify("Timeline `%s` already exists.", c.timeline.Name)
	case c.dryRun:
		return o.Notify("Would create timeline `%s`.", c.timeline.Name)
	}

	return o.Notify("Created timeline `%[1]s`.\nNext: coroner timeline use %[1]s\n      coroner event add -t %[1]s <text>", c.timeline.Name)
}

// AskCreate asks for a timeline through timeline create's own form, framed as s frames its asks
// under that form's title, and returns its name, and whether one was created or used.
func AskCreate(ctx context.Context, s clikit.FormSession) (string, bool, error) {
	s.Frame.Title = timelineCreateTitle
	t, created, err := askTimelineCreate(ctx, s, timelineCreateWrite(), clikit.FieldValues{})

	return t.Name, created, err
}
