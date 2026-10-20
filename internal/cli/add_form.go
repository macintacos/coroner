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
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// formWrite is a write command a form submits to: its path below coroner, definition and run function.
type formWrite[R renderer] struct {
	// path is the command below coroner, as in "event add".
	path string
	def  definition
	run  func(context.Context, App, config.Config, fieldValues) (R, error)
}

// addForm is an add command's form: the command it submits to, and what each entry picks from.
type addForm[R renderer] struct {
	formWrite[R]
	// choices reads what the form picks from once the timeline t is known, labelling times in z.
	choices func(ctx context.Context, s *store.Store, t timeline.Timeline, z zone) (formChoices, error)
}

// askFunc runs a form of groups and reports whether it was submitted: Esc returns false, Ctrl-C
// huh.ErrUserAborted.
type askFunc func(ctx context.Context, groups ...formGroup) (bool, error)

// formGroup is one page of a form: its fields, which the form skips while hide reports true.
type formGroup struct {
	fields []huh.Field
	// hide reports whether the form skips the group; nil never does.
	hide func() bool
}

// fieldsOf returns every field of groups, in order.
func fieldsOf(groups []formGroup) []huh.Field {
	var fields []huh.Field
	for _, g := range groups {
		fields = append(fields, g.fields...)
	}

	return fields
}

// newForm returns the form that asks groups in turn.
func newForm(groups ...formGroup) *huh.Form {
	pages := make([]*huh.Group, len(groups))
	for i, g := range groups {
		pages[i] = huh.NewGroup(g.fields...)
		if g.hide != nil {
			pages[i] = pages[i].WithHideFunc(g.hide)
		}
	}

	return huh.NewForm(pages...)
}

// errAskAgain is what a form's submission returns to ask the form again without printing an error.
var errAskAgain = errors.New("ask the form again")

// formSession is what one run of a write command's form reads and writes.
type formSession struct {
	app App
	inv invocation
	ask askFunc
	// thumbnails are what the form's list fields show under their values, by field name.
	thumbnails map[string]func(value string) (string, tea.Cmd)
	// placeholders are what the form's empty fields show as their default, by field name.
	placeholders map[string]string
}

const interactiveUsage = "Open a form to fill in, prefilled from the other flags (needs a terminal)"

// formCommand is a write command with a form.
type formCommand struct {
	def definition
	// ready reports whether the command line's values run without a form.
	ready func(in fieldValues) bool
	// flagsOnly runs the command line without a form.
	flagsOnly func(*cobra.Command, []string) error
	// session runs the form, prefilled from the command line's values.
	session func(s formSession, in fieldValues) error
}

// attach registers c's flags and --interactive on cmd, and a RunE that asks through forms when
// formMode opens the form, and otherwise runs flagsOnly.
func (c formCommand) attach(cmd *cobra.Command, app App, forms func(color bool, images graphics.Protocol) askFunc) *cobra.Command {
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		inv, err := readInvocation(cmd, app)
		if err != nil {
			return err
		}
		interactive, _ := cmd.Flags().GetBool("interactive")
		if interactive && !inv.terminal {
			return interactiveRefusal(cmd, inv.inputOff)
		}
		in := c.def.read(cmd.Flags(), args)
		if !formMode(formGate{interactive: interactive, ready: c.ready(in), machine: inv.out.format.machine(), terminal: inv.terminal}) {
			// ponytail: flagsOnly reads the invocation again; pass inv to flagsOnly if that read grows costly.
			return c.flagsOnly(cmd, args)
		}

		return c.session(newFormSession(app, inv, forms), in)
	}

	c.def.addFlags(cmd.Flags())
	cmd.Flags().Bool("interactive", false, interactiveUsage)

	return cmd
}

// attach registers f's flags and --interactive on cmd, and a RunE that asks through forms when a
// field the command requires is missing or --interactive asks, and otherwise runs writeRunE.
func (f addForm[R]) attach(
	ctx context.Context, cmd *cobra.Command, app App, forms func(color bool, images graphics.Protocol) askFunc,
) *cobra.Command {
	return formCommand{
		def:       f.def,
		ready:     func(in fieldValues) bool { return requiredGiven(f.def, in) },
		flagsOnly: writeRunE(ctx, app, f.def, f.run),
		session: func(s formSession, in fieldValues) error {
			return f.session(ctx, s, in)
		},
	}.attach(cmd, app, forms)
}

// interactiveRefusal returns the error --interactive on cmd gets with no terminal to open a form on:
// a conflict with --no-input or CORONER_NO_INPUT when inputOff, otherwise a missing terminal.
func interactiveRefusal(cmd *cobra.Command, inputOff bool) error {
	switch {
	case inputOff && cmd.Flags().Changed("no-input"):
		return &Error{
			Kind: KindUsage, Message: "--interactive conflicts with --no-input",
			Hint: "Pass --interactive or --no-input, not both.",
		}
	case inputOff:
		return &Error{
			Kind: KindUsage, Message: "--interactive conflicts with CORONER_NO_INPUT",
			Hint: "Unset CORONER_NO_INPUT, or pass every value as a flag.",
		}
	}

	return &Error{
		Kind: KindUsage, Message: "--interactive needs a terminal",
		Hint: "Run '" + cmd.CommandPath() + " --help' for the flags it takes.",
	}
}

// newFormSession returns the session a form runs in under inv, asking through forms.
func newFormSession(app App, inv invocation, forms func(color bool, images graphics.Protocol) askFunc) formSession {
	images := graphics.None
	if inv.outColor {
		images = graphics.Detect(app.LookupEnv)
	}

	placeholders := map[string]string{}
	author, err := defaultAuthor(nil, inv.cfg.Author, app.LookupEnv)
	if err == nil && author != nil {
		placeholders["author"] = *author
	}

	return formSession{
		app: app, inv: inv, ask: forms(inv.outColor, images),
		thumbnails:   map[string]func(string) (string, tea.Cmd){"attach": attachThumbnail(app.ReadFile, images)},
		placeholders: placeholders,
	}
}

// formGate is what formMode decides from: whether --interactive asks for the form, the command
// line's values run without it, a machine format was asked for, and a form may take over the
// terminal.
type formGate struct {
	interactive, ready, machine, terminal bool
}

// formMode reports whether a write command opens its form: on a terminal, when --interactive asks
// for it, or when the command line is not ready to run without it and no machine format was asked
// for, since a machine format's caller is a script.
func formMode(g formGate) bool {
	return g.terminal && (g.interactive || !g.ready && !g.machine)
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
		submitted, err := s.ask(ctx, formGroup{fields: []huh.Field{another}})
		if err != nil {
			return askError(err)
		}
		// Value can rebind the confirm, so read what it holds.
		accepted, _ := another.GetValue().(bool)
		if !submitted || !accepted {
			return nil
		}

		in = carryForward(typed, f.def)
	}
}

// chooseTimeline returns in with the timeline the session writes to, asking for one through def's
// timeline field when no layer selects it, and whether there is one: Esc on that ask leaves none.
func chooseTimeline(ctx context.Context, s formSession, def definition, in fieldValues) (fieldValues, bool, error) {
	var by selectedBy
	var timelines []timeline.Timeline
	err := withStorePath(ctx, s.app, s.inv.cfg.DBPath, func(st *store.Store) error {
		sel, err := selectTimelines(ctx, st, timelineInputOf(in), s.app.LookupEnv)
		if err != nil || sel.by != "" {
			by = sel.by
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

	submitted, err := s.ask(ctx, formGroup{fields: fields})
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

// entry asks for one entry, prefilled from in, and adds it to in's timeline once submitted. It returns
// the values last submitted, or in when none was.
func (f addForm[R]) entry(ctx context.Context, s formSession, in fieldValues) (fieldValues, error) {
	choices, err := f.entryChoices(ctx, s, in, baseCheckEnv(s).zone)
	if err != nil {
		return in, err
	}

	return s.askUntil(ctx, withoutField(f.def, "timeline"), choices, in, func(typed fieldValues) error {
		if in.given("timeline") {
			typed["timeline"] = in.values("timeline")
		}
		return f.submit(ctx, s, typed)
	})
}

// askUntil asks def's fields, prefilled from in and picking from choices, and passes what was typed to
// submit. It asks again with what was typed while submit returns errAskAgain, or a correctable error,
// which it prints. It returns what was last typed and submit's error, or in when Esc ends the form.
func (s formSession) askUntil(
	ctx context.Context, def definition, choices formChoices, in fieldValues, submit func(typed fieldValues) error,
) (fieldValues, error) {
	choices.thumbnails = s.thumbnails
	choices.placeholders = s.placeholders
	for {
		fields := def.formFields(baseCheckEnv(s), in, choices)
		groups := def.formGroups(fields, in)
		submitted, err := s.ask(ctx, groups...)
		switch {
		case err != nil:
			return in, askError(err)
		case !submitted:
			return in, nil
		}

		typed := def.formValues(def.shownFields(groups))
		err = submit(typed)
		rejected, _ := errors.AsType[*Error](err)
		switch {
		case errors.Is(err, errAskAgain):
		case correctable(err):
			// A rejected entry is reported to the person at the form, never as a JSON envelope.
			err = writeError(s.app.Err, rejected, errorStyle{color: s.inv.errColor})
			if err != nil {
				return typed, err
			}
		default:
			return typed, err
		}
		in = typed
	}
}

// entryChoices returns what an entry's fields pick from in the timeline in selects, labelling times
// in z.
func (f addForm[R]) entryChoices(ctx context.Context, s formSession, in fieldValues, z zone) (formChoices, error) {
	var choices formChoices
	err := withStorePath(ctx, s.app, s.inv.cfg.DBPath, func(st *store.Store) error {
		t, err := selectOne(ctx, st, timelineInputOf(in), s.app.LookupEnv)
		if err != nil {
			return err
		}
		choices, err = f.choices(ctx, st, t, z)
		return err
	})

	return choices, err
}

// correctable reports whether err rejects a submission the user can fix and submit again: usage, a
// taken timeline name, or a record not found other than the timeline the form writes to.
func correctable(err error) bool {
	rejected, ok := errors.AsType[*Error](err)
	return ok && (rejected.Kind.ExitCode() == 2 || rejected.Kind.ExitCode() == 3 || rejected.Kind == KindTimelineExists) &&
		rejected.Kind != KindTimelineNotFound && rejected.Kind != KindTimelineRequired
}

// submit runs values as the command line would, and prints the result and the flags-only command it
// equals, against the same database.
func (w formWrite[R]) submit(ctx context.Context, s formSession, values fieldValues) error {
	result, err := w.run(ctx, s.app, s.inv.cfg, values)
	if err != nil {
		return err
	}
	err = result.render(s.inv.out)
	if err != nil {
		return err
	}

	return s.echo(w.path, w.def, values)
}

// echo prints the flags-only command that gives values to the command at path, with def's
// fields, against the same database.
func (s formSession) echo(path string, def definition, values fieldValues) error {
	args := commandArgs(path, def, values)
	return s.inv.out.notify("Equivalent command: %s", shellLine(slices.Concat(args[:1], s.inv.echoArgs, args[1:])))
}

// baseCheckEnv is what a form's checks read time input against before a zone is typed into it:
// now, in --tz, else the config file's default_timezone, or the local zone when that is invalid,
// which the add reports where it reads it.
func baseCheckEnv(s formSession) checkEnv {
	now := s.app.Now()
	z, err := timeZone(s.inv.tz, s.inv.cfg.DefaultTimezone, now.Location())
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

// carryForward returns values without d's perEntry fields, as the next entry opens with.
func carryForward(values fieldValues, d definition) fieldValues {
	next := maps.Clone(values)
	for _, f := range d.fields {
		if f.perEntry {
			delete(next, f.name)
		}
	}

	return next
}

func withoutField(d definition, name string) definition {
	return definition{fields: slices.DeleteFunc(slices.Clone(d.fields), func(f field) bool { return f.name == name })}
}

func onlyField(d definition, name string) definition {
	return definition{fields: slices.DeleteFunc(slices.Clone(d.fields), func(f field) bool { return f.name != name })}
}

// terminalForms returns the askFunc that runs forms on app's terminal, drawing in color when color
// is true, in the profile an attach field needs for the images the terminal shows.
func terminalForms(app App) func(color bool, images graphics.Protocol) askFunc {
	return func(color bool, images graphics.Protocol) askFunc {
		return func(ctx context.Context, groups ...formGroup) (bool, error) {
			fields := fieldsOf(groups)
			attach := slices.ContainsFunc(fields, func(f huh.Field) bool { return f.GetKey() == "attach" })
			profile := formProfile(color, images, attach)
			cancelled := false
			// The caller's context already ends on a signal, and the profile is pinned so bubbletea
			// never reads the process environment to pick one.
			err := newForm(groups...).WithInput(app.In).WithOutput(app.Out).
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
		choices: func(ctx context.Context, s *store.Store, _ timeline.Timeline, _ zone) (formChoices, error) {
			return sourceChoices(ctx, s)
		},
	}
}

func annotationAddForm() addForm[annotationAdded] {
	return addForm[annotationAdded]{
		path: "annotation add", def: annotationAddDefinition(), run: addAnnotation,
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
