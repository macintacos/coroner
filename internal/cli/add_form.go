package cli

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// addForm is what an add command's form needs beyond its definition.
type addForm[R interface{ render(o output) error }] struct {
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
}

const interactiveUsage = "Open a form to fill in, prefilled from the other flags (needs a terminal)"

// attach makes cmd the add command f describes: its flags, --interactive, and a RunE that opens
// the form forms builds when formMode says so, and otherwise runs the command line as writeRunE does.
func (f addForm[R]) attach(ctx context.Context, cmd *cobra.Command, app App, forms func(color bool) askFunc) *cobra.Command {
	flagsOnly := writeRunE(ctx, app, f.def, f.run)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		interactive, _ := cmd.Flags().GetBool("interactive")
		open, err := formMode(interactive, len(args) > 0, interactiveTerminal(app))
		switch {
		case err != nil:
			return err
		case !open:
			return flagsOnly(cmd, args)
		}

		o, err := newOutput(cmd, app)
		if err != nil {
			return err
		}
		cfg, err := resolveConfig(cmd, app)
		if err != nil {
			return err
		}
		noColor, _ := cmd.Flags().GetBool("no-color")
		s := formSession{app: app, cfg: cfg, o: o, ask: forms(colorEnabled(app.OutIsTTY, noColor, app.LookupEnv))}

		return f.session(ctx, s, f.def.read(cmd.Flags(), args))
	}

	f.def.addFlags(cmd.Flags())
	cmd.Flags().Bool("interactive", false, interactiveUsage)

	return cmd
}

// interactiveTerminal reports whether a command may take over the terminal: keys can arrive, and
// the screen is a terminal that can draw (I-1).
func interactiveTerminal(app App) bool {
	term, _ := app.LookupEnv("TERM")
	return app.InIsTTY && app.OutIsTTY && term != "dumb"
}

// formMode reports whether an add command opens its form: on a terminal, when --interactive asks
// for it or its positional argument is missing. --interactive without a terminal is usage (A-6).
func formMode(interactive, argGiven, terminal bool) (bool, error) {
	if interactive && !terminal {
		return false, &Error{
			Kind:    KindUsage,
			Message: "--interactive needs a terminal on stdin and stdout",
			Hint:    "Pass the flags instead; --help lists them.",
		}
	}

	return terminal && (interactive || !argGiven), nil
}

// session asks for entries, prefilled from in, adding each one submitted, until no other is wanted.
func (f addForm[R]) session(ctx context.Context, s formSession, in fieldValues) error {
	for {
		typed, err := f.entry(ctx, s, in)
		if err != nil {
			return err
		}

		noun, _, _ := strings.Cut(f.path, " ")
		another := huh.NewConfirm().Title("Add another " + noun + "?").Value(new(true))
		submitted, err := s.ask(ctx, another)
		if err != nil || !submitted || another.GetValue() != true {
			return askError(ctx, err)
		}

		in = carryForward(typed, f.cleared)
	}
}

// entry asks for one entry, prefilled from in, and adds it once submitted, asking again with what was
// typed while the add rejects it. It returns the values last typed, added or not.
func (f addForm[R]) entry(ctx context.Context, s formSession, in fieldValues) (fieldValues, error) {
	env, err := baseCheckEnv(s)
	if err != nil {
		return in, err
	}
	in, choices, err := f.prepare(ctx, s, in, env)
	if err != nil || in == nil {
		return in, err
	}

	def := withoutField(f.def, "timeline")
	for {
		fields := def.formFields(env, in, choices)
		submitted, err := s.ask(ctx, fields...)
		if err != nil || !submitted {
			return in, askError(ctx, err)
		}

		typed := def.formValues(fields)
		if in.given("timeline") {
			typed["timeline"] = in.values("timeline")
		}
		err = f.add(ctx, s, typed)
		rejected, ok := errors.AsType[*Error](err)
		if !ok || (rejected.Kind.ExitCode() != 2 && rejected.Kind.ExitCode() != 3) {
			return typed, err
		}

		style, _ := errorStyleFor(s.app)
		err = writeError(s.app.Err, rejected, style)
		if err != nil {
			return typed, err
		}
		in = typed
	}
}

// prepare returns in with the timeline the entry goes to, asking for one when no layer selects it,
// and what the entry's fields pick from. It returns nil values when the timeline ask was cancelled.
func (f addForm[R]) prepare(ctx context.Context, s formSession, in fieldValues, env checkEnv) (fieldValues, formChoices, error) {
	var choices formChoices
	var timelines []timeline.Timeline
	err := withStorePath(ctx, s.app, s.cfg.DBPath, func(st *store.Store) error {
		t, by, err := lookupTimeline(ctx, st, in.value("timeline"), s.app.LookupEnv)
		switch {
		case err != nil:
			return err
		case by != "":
			choices, err = f.choices(ctx, st, t, env.zone)
			return err
		}

		timelines, err = st.Timelines(ctx)
		if err == nil && len(timelines) == 0 {
			return timelineRequiredError(ctx, st)
		}
		return err
	})
	if err != nil || timelines == nil {
		return in, choices, err
	}

	picked, err := f.askTimeline(ctx, s, timelines, env)
	if err != nil || picked == "" {
		return nil, choices, err
	}
	in = maps.Clone(in)
	in["timeline"] = []string{picked}

	return f.prepare(ctx, s, in, env)
}

// askTimeline asks which of timelines the entry goes to, through the definition's own timeline
// field, and returns its name, or "" when the ask was cancelled.
func (f addForm[R]) askTimeline(ctx context.Context, s formSession, timelines []timeline.Timeline, env checkEnv) (string, error) {
	options := make([]huh.Option[string], 0, len(timelines))
	for _, t := range timelines {
		options = append(options, huh.NewOption(strings.TrimSpace(t.Name+"  "+t.Title), t.Name))
	}
	def := definition{fields: slices.DeleteFunc(slices.Clone(f.def.fields), func(f field) bool { return f.name != "timeline" })}
	fields := def.formFields(env, fieldValues{}, formChoices{options: map[string][]huh.Option[string]{"timeline": options}})

	submitted, err := s.ask(ctx, fields...)
	if err != nil || !submitted {
		return "", askError(ctx, err)
	}

	return def.formValues(fields).values("timeline")[0], nil
}

// add runs the entry's values as the command line would, prints what it added and the
// flags-only command that adds the same.
func (f addForm[R]) add(ctx context.Context, s formSession, values fieldValues) error {
	result, err := f.run(ctx, s.app, s.cfg, values)
	if err != nil {
		return err
	}
	err = result.render(s.o)
	if err != nil {
		return err
	}

	return s.o.notify("Equivalent command: %s", shellLine(commandArgs(f.path, f.def, values)))
}

// baseCheckEnv is what a form's checks read time input against before a zone is typed into it:
// now, in the config file's default_timezone.
func baseCheckEnv(s formSession) (checkEnv, error) {
	now := s.app.Now()
	z, err := timeZone(nil, s.cfg.DefaultTimezone, now.Location())

	return checkEnv{now: now, zone: z}, err
}

// askError returns err as the command reports it: Ctrl-C, or the end of ctx, is interrupted.
func askError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, huh.ErrUserAborted) {
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: errors.Join(err, ctx.Err())}
	}

	return err
}

// carryForward returns values without the fields in cleared, as the next entry opens with.
func carryForward(values fieldValues, cleared []string) fieldValues {
	next := maps.Clone(values)
	maps.DeleteFunc(next, func(name string, _ []string) bool { return slices.Contains(cleared, name) })

	return next
}

// withoutField returns d without the field called name.
func withoutField(d definition, name string) definition {
	return definition{fields: slices.DeleteFunc(slices.Clone(d.fields), func(f field) bool { return f.name == name })}
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
				WithProgramOptions(tea.WithFilter(escCancels(&cancelled)), tea.WithColorProfile(profile), tea.WithoutSignalHandler()).
				RunWithContext(ctx)
			if cancelled {
				return false, nil
			}

			return err == nil, err
		}
	}
}

// escCancels returns a program filter that turns Esc into the form's own abort, so the form clears
// itself from the screen, and records in cancelled that Esc, not Ctrl-C, ended it.
func escCancels(cancelled *bool) func(tea.Model, tea.Msg) tea.Msg {
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if key, ok := msg.(tea.KeyPressMsg); ok && key.Code == tea.KeyEscape {
			*cancelled = true
			return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
		}

		return msg
	}
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

	eventOptions := []huh.Option[string]{huh.NewOption("none (the whole timeline)", "")}
	for _, e := range events {
		seq := strconv.FormatInt(e.Seq, 10)
		eventOptions = append(eventOptions, huh.NewOption("#"+seq+"  "+occurredText(e, z)+"  "+cut(e.Text, 60), seq))
	}
	choices.options = map[string][]huh.Option[string]{
		"event": eventOptions,
		"kind": {
			huh.NewOption(string(timeline.AnnotationEvidence), string(timeline.AnnotationEvidence)),
			huh.NewOption(string(timeline.AnnotationComment), string(timeline.AnnotationComment)),
		},
	}

	return choices, nil
}

// cut returns text's first line, shortened to at most n runes with an ellipsis.
func cut(text string, n int) string {
	line, _, _ := strings.Cut(text, "\n")
	if utf8.RuneCountInString(line) <= n {
		return line
	}

	return string([]rune(line)[:n-1]) + "…"
}
