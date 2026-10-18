package cli

import (
	"context"
	"errors"
	"slices"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineCreateCommand(ctx context.Context, app App) *cobra.Command {
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
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline create mongo-outage-2026-09-11 --title \"Primary election storm\" \\\n" +
			"      --ref INC-4821 --ref https://jira.example.com/browse/INC-4821\n" +
			"  $ coroner timeline create mongo-outage-2026-09-11 --if-not-exists --json\n" +
			"  $ coroner timeline create --interactive",
	}

	return timelineCreateForm(ctx, app).attach(cmd, app, terminalForms(app))
}

// timelineCreateForm is timeline create with its form, which opens on a terminal when the name is
// missing.
func timelineCreateForm(ctx context.Context, app App) formCommand {
	create := formWrite[timelineCreated]{path: "timeline create", def: timelineCreateDefinition(), run: createTimeline}
	return formCommand{
		def:       create.def,
		ready:     func(in fieldValues) bool { return requiredGiven(create.def, in) },
		flagsOnly: withOneArg(writeRunE(ctx, app, create.def, create.run)),
		session: func(_ *cobra.Command, s formSession, in fieldValues) error {
			return askTimelineCreate(ctx, s, create, in)
		},
	}
}

// askTimelineCreate asks for the timeline to create, prefilled from in, until one is created or
// used, or Esc ends the form. A taken name offers the existing timeline, or uses it when in gives
// --if-not-exists.
func askTimelineCreate(ctx context.Context, s formSession, create formWrite[timelineCreated], in fieldValues) error {
	taken, err := timelineNames(ctx, s)
	if err != nil {
		return err
	}
	ifNotExists, _ := in.boolean("if-not-exists")
	note := func(name string) string { return "Timeline `" + name + "` exists; submitting offers to use it." }
	if ifNotExists {
		note = func(name string) string { return "Timeline `" + name + "` exists; submitting uses it." }
	}
	def := withoutField(create.def, "if-not-exists")
	def.fields[slices.IndexFunc(def.fields, func(f field) bool { return f.name == "name" })].preview = namePreview(taken, note)

	_, err = s.askUntil(ctx, def, formChoices{}, in, func(typed fieldValues) error {
		if ifNotExists {
			typed["if-not-exists"] = []string{"true"}
		}
		err := create.submit(ctx, s, typed)
		rejected, _ := errors.AsType[*Error](err)
		if rejected != nil && rejected.Kind == KindTimelineExists {
			return useExisting(ctx, s, create, typed)
		}
		return err
	})

	return err
}

// useExisting asks whether to use the existing timeline typed names, and prints it as
// --if-not-exists does when so. Picking another name returns errAskAgain; Esc writes nothing.
func useExisting(ctx context.Context, s formSession, create formWrite[timelineCreated], typed fieldValues) error {
	use := true
	confirm := huh.NewConfirm().Title("Timeline `" + *typed.value("name") + "` already exists").
		Affirmative("Use it").Negative("Pick another name").Value(&use)
	submitted, err := s.ask(ctx, confirm)
	switch {
	case err != nil:
		return askError(err)
	case !submitted:
		return nil
	}
	// Value can rebind the confirm, so read what it holds.
	if use, _ = confirm.GetValue().(bool); !use {
		return errAskAgain
	}

	typed["if-not-exists"] = []string{"true"}
	return create.submit(ctx, s, typed)
}

// timelineNames returns the names of the timelines in the session's database.
func timelineNames(ctx context.Context, s formSession) ([]string, error) {
	var names []string
	err := withStorePath(ctx, s.app, s.cfg.DBPath, func(st *store.Store) error {
		timelines, err := st.Timelines(ctx)
		for _, t := range timelines {
			names = append(names, t.Name)
		}
		return err
	})

	return names, err
}

// namePreview returns the preview of a timeline name field: checkName's hint for a rejected name,
// note for one of taken, and nothing otherwise.
func namePreview(taken []string, note func(name string) string) func(string, checkEnv) string {
	return func(name string, _ checkEnv) string {
		rejected, ok := errors.AsType[*Error](checkName(name))
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

func timelineCreateDefinition() definition {
	return definition{fields: []field{
		{
			name: "name", usage: "Timeline `name`, which commands select it by", arg: true, required: true,
			check: withoutEnv(checkName), suggest: suggestName,
		},
		{name: "title", usage: "Free-text `title`"},
		{name: "ref", typ: stringsField, list: true, usage: "Ticket key or URL the timeline belongs to; repeat for more"},
		{name: "if-not-exists", typ: boolField, usage: "Print the existing timeline instead of failing when the name is taken"},
		{name: "dry-run", typ: boolField, usage: "Show the timeline as it would be created, and create nothing"},
	}}
}

// timelineCreated is the timeline create made or, with --dry-run, would make, or the one
// --if-not-exists found.
type timelineCreated struct {
	timeline        timeline.Timeline
	existed, dryRun bool
}

func createTimeline(ctx context.Context, app App, cfg config.Config, in fieldValues) (timelineCreated, error) {
	// Before the checks, so a rejected name still uses an ID, as the contract scripts number them.
	id, err := app.NewID()
	if err != nil {
		return timelineCreated{}, err
	}
	err = timelineCreateDefinition().check(in, checkEnv{})
	if err != nil {
		return timelineCreated{}, err
	}
	name := in.value("name")
	if name == nil {
		return timelineCreated{}, missingValue("timeline create", "the timeline's name")
	}
	now := app.Now()
	t := timeline.Timeline{
		ID: id.String(), Name: *name, Refs: appendUnique(nil, in.values("ref")...),
		CreatedAt: now, UpdatedAt: now,
	}
	if title := in.value("title"); title != nil {
		t.Title = *title
	}
	err = checkTimeline(t)
	if err != nil {
		return timelineCreated{}, err
	}
	ifNotExists, _ := in.boolean("if-not-exists")
	dryRun, _ := in.boolean("dry-run")

	created := timelineCreated{timeline: t, dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		if dryRun {
			err = timelineNameFree(ctx, s, t.Name)
		} else {
			err = s.CreateTimeline(ctx, t)
		}
		switch {
		case errors.Is(err, store.ErrConflict) && ifNotExists:
			created.timeline, err = s.TimelineByName(ctx, t.Name)
			created.existed = true
			return err
		case errors.Is(err, store.ErrConflict):
			return timelineExistsError(t.Name, "Pick another name, or pass --if-not-exists to use the existing timeline.", err)
		}

		return err
	})

	return created, err
}

// timelineNameFree returns store.ErrConflict when a timeline is called name.
func timelineNameFree(ctx context.Context, s *store.Store, name string) error {
	_, err := s.TimelineByName(ctx, name)
	switch {
	case err == nil:
		return store.ErrConflict
	case errors.Is(err, store.ErrNotFound):
		return nil
	}

	return err
}

func (c timelineCreated) render(o output) error {
	err := printTimeline(o, c.timeline)
	if err == nil && !c.dryRun {
		err = o.printID(c.timeline.ID)
	}
	if err != nil {
		return err
	}
	switch {
	case c.existed:
		return o.notify("Timeline `%s` already exists.", c.timeline.Name)
	case c.dryRun:
		return o.notify("Would create timeline `%s`.", c.timeline.Name)
	}

	return o.notify("Created timeline `%[1]s`.\nNext: coroner timeline use %[1]s\n      coroner event add -t %[1]s <text>", c.timeline.Name)
}
