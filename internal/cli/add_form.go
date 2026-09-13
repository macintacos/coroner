package cli

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// addForm is an add command's definition and what its form needs besides.
type addForm[R renderer] struct {
	// path is the command below coroner, as in "event add".
	path string
	def  definition
	// cleared are the fields another entry opens without.
	cleared []string
	run     func(context.Context, App, config.Config, fieldValues) (R, error)
	// choices reads what the form picks from once the timeline t is known, labelling times in z.
	choices func(ctx context.Context, s *store.Store, t timeline.Timeline, z zone) (formChoices, error)
}

// askFunc runs a form of fields and reports whether it was submitted: Esc returns false, Ctrl-C
// huh.ErrUserAborted.
type askFunc func(ctx context.Context, fields ...huh.Field) (bool, error)

// formSession is what one run of an add command's form reads and writes.
type formSession struct {
	app App
	cfg config.Config
	o   output
	ask askFunc
	// global are the --db and --config args the command line gave, which each echoed command repeats.
	global []string
	style  errorStyle
}

const interactiveUsage = "Open a form to fill in, prefilled from the other flags (needs a terminal)"

// formCommand is a write command with a form: its definition, when its command line runs without
// the form, what runs it then, and the form's session.
type formCommand struct {
	def definition
	// ready reports whether the command line's values run without a form.
	ready     func(in fieldValues) bool
	flagsOnly func(*cobra.Command, []string) error
	session   func(cmd *cobra.Command, s formSession, in fieldValues) error
}

// attach registers c's flags and --interactive on cmd, and a RunE that asks through forms when
// formMode opens the form, and otherwise runs flagsOnly.
func (c formCommand) attach(cmd *cobra.Command, app App, forms func(color bool) askFunc) *cobra.Command {
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		interactive, _ := cmd.Flags().GetBool("interactive")
		terminal := interactiveTerminal(app)
		if interactive && !terminal {
			return &Error{
				Kind: KindUsage, Message: "--interactive needs a terminal",
				Hint: "Run '" + cmd.CommandPath() + " --help' for the flags it takes.",
			}
		}
		in := c.def.read(cmd.Flags(), args)
		if !formMode(interactive, c.ready(in), terminal) {
			return c.flagsOnly(cmd, args)
		}

		s, err := newFormSession(cmd, app, forms)
		if err != nil {
			return err
		}

		return c.session(cmd, s, in)
	}

	c.def.addFlags(cmd.Flags())
	cmd.Flags().Bool("interactive", false, interactiveUsage)

	return cmd
}

// attach registers f's flags and --interactive on cmd, and a RunE that asks through forms when a
// field the command requires is missing or --interactive asks, and otherwise runs writeRunE.
func (f addForm[R]) attach(ctx context.Context, cmd *cobra.Command, app App, forms func(color bool) askFunc) *cobra.Command {
	return formCommand{
		def:       f.def,
		ready:     func(in fieldValues) bool { return requiredGiven(f.def, in) },
		flagsOnly: writeRunE(ctx, app, f.def, f.run),
		session: func(_ *cobra.Command, s formSession, in fieldValues) error {
			return f.session(ctx, s, in)
		},
	}.attach(cmd, app, forms)
}

// newFormSession returns the session cmd's form runs in, asking through forms.
func newFormSession(cmd *cobra.Command, app App, forms func(color bool) askFunc) (formSession, error) {
	o, err := newOutput(cmd, app)
	if err != nil {
		return formSession{}, err
	}
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return formSession{}, err
	}

	var global []string
	for _, name := range []string{"db", "config"} {
		if value := changedString(cmd.Flags(), name); value != nil {
			global = append(global, "--"+name, *value)
		}
	}
	noColor, _ := cmd.Flags().GetBool("no-color")

	return formSession{
		app: app, cfg: cfg, o: o, ask: forms(colorEnabled(app.OutIsTTY, noColor, app.LookupEnv)), global: global,
		// A rejected entry is reported to the person at the form, never as a JSON envelope.
		style: errorStyle{color: colorEnabled(app.ErrIsTTY, noColor, app.LookupEnv)},
	}, nil
}

// interactiveTerminal reports whether a command may take over the terminal: keys can arrive, and
// the screen is a terminal that can draw (I-1).
func interactiveTerminal(app App) bool {
	term, _ := app.LookupEnv("TERM")
	return app.InIsTTY && app.OutIsTTY && term != "dumb"
}

// formMode reports whether a write command opens its form: on a terminal, when --interactive asks
// for it or the command line is not ready to run without it.
func formMode(interactive, ready, terminal bool) bool {
	return terminal && (interactive || !ready)
}

// requiredGiven reports whether in gives every field d requires.
func requiredGiven(d definition, in fieldValues) bool {
	return !slices.ContainsFunc(d.fields, func(f field) bool { return f.required && !in.given(f.name) })
}

// session asks for entries, prefilled from in, adding each one submitted, until no other is wanted.
func (f addForm[R]) session(ctx context.Context, s formSession, in fieldValues) error {
	in, chosen, err := chooseTimeline(ctx, s, f.def, in)
	if err != nil || !chosen {
		return err
	}

	noun, _, _ := strings.Cut(f.path, " ")
	for {
		typed, err := f.entry(ctx, s, in)
		if err != nil {
			return err
		}

		yes := true
		another := huh.NewConfirm().Title("Add another " + noun + "?").Value(&yes)
		submitted, err := s.ask(ctx, another)
		if err != nil {
			return askError(err)
		}
		// Value can rebind the confirm, so read what it holds.
		accepted, _ := another.GetValue().(bool)
		if !submitted || !accepted {
			return nil
		}

		in = carryForward(typed, f.cleared)
	}
}

// chooseTimeline returns in with the timeline the session writes to, asking for one through def's
// timeline field when no layer selects it, and whether there is one: Esc on that ask leaves none.
func chooseTimeline(ctx context.Context, s formSession, def definition, in fieldValues) (fieldValues, bool, error) {
	var by selectedBy
	var timelines []timeline.Timeline
	err := withStorePath(ctx, s.app, s.cfg.DBPath, func(st *store.Store) error {
		var err error
		_, by, err = lookupTimeline(ctx, st, in.value("timeline"), s.app.LookupEnv)
		if err != nil || by != "" {
			return err
		}

		timelines, err = st.Timelines(ctx)
		if err == nil && len(timelines) == 0 {
			return timelineRequiredError(ctx, st)
		}
		return err
	})
	switch {
	case err != nil:
		return in, false, err
	case by != "":
		return in, true, nil
	}

	return askTimeline(ctx, s, def, in, timelines)
}

// askTimeline asks which of timelines in goes to, through def's timeline field, and returns in with
// it, and whether one was picked.
func askTimeline(
	ctx context.Context, s formSession, def definition, in fieldValues, timelines []timeline.Timeline,
) (fieldValues, bool, error) {
	options := make([]huh.Option[string], 0, len(timelines))
	for _, t := range timelines {
		options = append(options, huh.NewOption(strings.TrimSpace(t.Name+"  "+pickerLabel(t.Title)), t.Name))
	}
	pick := onlyField(def, "timeline")
	fields := pick.formFields(baseCheckEnv(s), fieldValues{}, formChoices{options: map[string][]huh.Option[string]{"timeline": options}})

	submitted, err := s.ask(ctx, fields...)
	switch {
	case err != nil:
		return in, false, askError(err)
	case !submitted:
		return in, false, nil
	}

	picked := maps.Clone(in)
	picked["timeline"] = pick.formValues(fields).values("timeline")

	return picked, true, nil
}

// entry asks for one entry, prefilled from in, and adds it once submitted, asking again with what was
// typed while the add rejects it as correctable. It returns the values last submitted, or in when
// none was.
func (f addForm[R]) entry(ctx context.Context, s formSession, in fieldValues) (fieldValues, error) {
	env := baseCheckEnv(s)
	choices, err := f.entryChoices(ctx, s, in, env.zone)
	if err != nil {
		return in, err
	}

	def := withoutField(f.def, "timeline")
	for {
		fields := def.formFields(env, in, choices)
		submitted, err := s.ask(ctx, fields...)
		switch {
		case err != nil:
			return in, askError(err)
		case !submitted:
			return in, nil
		}

		typed := def.formValues(fields)
		if in.given("timeline") {
			typed["timeline"] = in.values("timeline")
		}
		err = f.add(ctx, s, typed)
		if err == nil || !correctable(err) {
			return typed, err
		}

		rejected, _ := errors.AsType[*Error](err)
		err = writeError(s.app.Err, rejected, s.style)
		if err != nil {
			return typed, err
		}
		in = typed
	}
}

// entryChoices returns what an entry's fields pick from in the timeline in selects, labelling times
// in z.
func (f addForm[R]) entryChoices(ctx context.Context, s formSession, in fieldValues, z zone) (formChoices, error) {
	var choices formChoices
	err := withStorePath(ctx, s.app, s.cfg.DBPath, func(st *store.Store) error {
		t, err := selectTimeline(ctx, st, in.value("timeline"), s.app.LookupEnv)
		if err != nil {
			return err
		}
		choices, err = f.choices(ctx, st, t, z)
		return err
	})

	return choices, err
}

// correctable reports whether err rejects an entry the user can fix and submit again: usage, or a
// record not found, except the timeline, which the entry has no field for.
func correctable(err error) bool {
	rejected, ok := errors.AsType[*Error](err)
	return ok && (rejected.Kind.ExitCode() == 2 || rejected.Kind.ExitCode() == 3) &&
		rejected.Kind != KindTimelineNotFound && rejected.Kind != KindTimelineRequired
}

// add runs the entry's values as the command line would, prints what it added and the
// flags-only command that adds the same, to the same database.
func (f addForm[R]) add(ctx context.Context, s formSession, values fieldValues) error {
	result, err := f.run(ctx, s.app, s.cfg, values)
	if err != nil {
		return err
	}
	err = result.render(s.o)
	if err != nil {
		return err
	}

	return s.echo(f.path, f.def, values)
}

// echo prints the flags-only command that gives values to the command at path, with def's
// fields, against the same database.
func (s formSession) echo(path string, def definition, values fieldValues) error {
	args := commandArgs(path, def, values)
	return s.o.notify("Equivalent command: %s", shellLine(slices.Concat(args[:1], s.global, args[1:])))
}

// baseCheckEnv is what a form's checks read time input against before a zone is typed into it:
// now, in the config file's default_timezone, or the local zone when that is invalid, which the
// add reports where it reads it.
func baseCheckEnv(s formSession) checkEnv {
	now := s.app.Now()
	z, err := timeZone(nil, s.cfg.DefaultTimezone, now.Location())
	if err != nil {
		z = zone{loc: now.Location(), label: "local"}
	}

	return checkEnv{now: now, zone: z}
}

// askError returns err as the command reports it: Ctrl-C is interrupted.
func askError(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: err}
	}

	return err
}

// carryForward returns values without the fields in cleared, as the next entry opens with.
func carryForward(values fieldValues, cleared []string) fieldValues {
	next := maps.Clone(values)
	maps.DeleteFunc(next, func(name string, _ []string) bool { return slices.Contains(cleared, name) })

	return next
}

func withoutField(d definition, name string) definition {
	return definition{fields: slices.DeleteFunc(slices.Clone(d.fields), func(f field) bool { return f.name == name })}
}

func onlyField(d definition, name string) definition {
	return definition{fields: slices.DeleteFunc(slices.Clone(d.fields), func(f field) bool { return f.name != name })}
}

// terminalForms returns the askFunc that runs forms on app's terminal, drawing in color when color
// is true.
func terminalForms(app App) func(color bool) askFunc {
	return func(color bool) askFunc {
		profile := colorprofile.ASCII
		if color {
			profile = colorprofile.ANSI
		}

		return func(ctx context.Context, fields ...huh.Field) (bool, error) {
			cancelled := false
			// The caller's context already ends on a signal, and the profile is pinned so bubbletea
			// never reads the process environment to pick one.
			err := huh.NewForm(huh.NewGroup(fields...)).WithInput(app.In).WithOutput(app.Out).
				WithProgramOptions(tea.WithFilter(escCancels(&cancelled, fields)), tea.WithColorProfile(profile), tea.WithoutSignalHandler()).
				RunWithContext(ctx)
			if cancelled {
				return false, nil
			}

			return err == nil, err
		}
	}
}

// escCancels returns a program filter that turns Esc into the form's own abort, so the form clears
// itself from the screen, and records in cancelled that Esc, not Ctrl-C, ended it. While one of
// fields filters its options, Esc is that field's.
func escCancels(cancelled *bool, fields []huh.Field) func(tea.Model, tea.Msg) tea.Msg {
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		key, ok := msg.(tea.KeyPressMsg)
		if !ok || key.Code != tea.KeyEscape || slices.ContainsFunc(fields, filtering) {
			return msg
		}

		*cancelled = true
		// ctrl+c is huh's Quit binding.
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
}

// filtering reports whether formField is a Select filtering its options.
func filtering(formField huh.Field) bool {
	f, ok := formField.(interface{ GetFiltering() bool })
	return ok && f.GetFiltering()
}

func eventAddForm() addForm[eventAdded] {
	return addForm[eventAdded]{
		path: "event add", def: eventAddDefinition(), run: addEvent,
		cleared: []string{"text", "at", "ended", "approximate", "url"},
		choices: func(ctx context.Context, s *store.Store, _ timeline.Timeline, _ zone) (formChoices, error) {
			return sourceChoices(ctx, s)
		},
	}
}

func annotationAddForm() addForm[annotationAdded] {
	return addForm[annotationAdded]{
		path: "annotation add", def: annotationAddDefinition(), run: addAnnotation,
		cleared: []string{"body", "url"},
		choices: annotationChoices,
	}
}

// sourceChoices suggests the sources there are for the source field.
func sourceChoices(ctx context.Context, s *store.Store) (formChoices, error) {
	sources, err := s.Sources(ctx)
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.Name)
	}

	return formChoices{suggestions: map[string][]string{"source": names}}, err
}

// annotationChoices suggests sources, picks the kind, and picks the event from t's events, or none
// for the whole timeline.
func annotationChoices(ctx context.Context, s *store.Store, t timeline.Timeline, z zone) (formChoices, error) {
	choices, err := sourceChoices(ctx, s)
	if err != nil {
		return choices, err
	}
	events, err := s.Events(ctx, t.ID)
	if err != nil {
		return choices, err
	}
	// ponytail: huh draws every option on each key, so only the last 200 events are offered; a
	// filtered query replaces the cap when older ones need picking. A typed -e still reaches any.
	events = events[max(0, len(events)-200):]

	eventOptions := []huh.Option[string]{huh.NewOption("none (the whole timeline)", "")}
	for _, e := range events {
		seq := strconv.FormatInt(e.Seq, 10)
		eventOptions = append(eventOptions, huh.NewOption("#"+seq+"  "+occurredText(e, z)+"  "+pickerLabel(e.Text), seq))
	}
	choices.options = map[string][]huh.Option[string]{
		"event": eventOptions,
		"kind": {
			huh.NewOption("choose evidence or comment", ""),
			huh.NewOption(string(timeline.AnnotationEvidence), string(timeline.AnnotationEvidence)),
			huh.NewOption(string(timeline.AnnotationComment), string(timeline.AnnotationComment)),
		},
	}

	return choices, nil
}

// pickerLabel returns stored text as one line a picker can draw, so it cannot move the cursor or
// send the terminal a sequence: no escape sequences or control characters, white space collapsed,
// at most 60 cells wide.
func pickerLabel(text string) string {
	printable := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, ansi.Strip(text))

	return ansi.Truncate(strings.Join(strings.Fields(printable), " "), 60, "…")
}
