package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
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
type addForm[R addResult] struct {
	formWrite[R]
	// choices reads what the form picks from once the timeline t is known, labelling times in z.
	choices func(ctx context.Context, s *store.Store, t timeline.Timeline, z zone) (formChoices, error)
}

// addResult is what an add form's command returns for an entry: rendered once submitted, and listed
// in the summary that ends the session when the add stored a record.
type addResult interface {
	renderer
	// summary returns how the session's summary lists the record, and whether the add stored it.
	summary() (summaryLine, bool)
}

// summaryLine is a record as the summary that ends an add form's session lists it: its timeline's
// name, and the cells of its row under the count, its text last.
type summaryLine struct {
	timeline string
	cells    []string
}

// askFunc runs a form of groups in frame and reports whether it was submitted: Esc returns false,
// Ctrl-C huh.ErrUserAborted.
type askFunc func(ctx context.Context, frame formFrame, groups ...formGroup) (bool, error)

// formFrame is what an ask's frame shows beside the form: a title in place of the command's, and an
// add session's timeline and the noun records it added, in the frame's edges.
type formFrame struct {
	title    string
	timeline string
	// entry is whether the form is an entry, which ctrl+t and Esc leave.
	entry bool
	added int
	noun  string
}

// edges returns what f's frame sets into its edges, titled title unless f names another: the
// timeline, and the count of records added once there is one, each with the key that leaves an
// entry for it.
func (f formFrame) edges(title string) frameEdges {
	edges := frameEdges{title: cmp.Or(f.title, title)}
	if f.timeline != "" {
		edges.top = []string{"📒 " + f.timeline}
		if f.entry {
			edges.top = append(edges.top, keyHint(switchTimelineKey()))
		}
	}
	if f.added > 0 {
		done := lipgloss.NewStyle().Foreground(lipgloss.Green).Render("✓")
		edges.bottom = []string{done + " " + strconv.Itoa(f.added) + " " + pluralNoun(f.added, f.noun) + " added"}
		if f.entry {
			edges.bottom = append(edges.bottom, keyHint(key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "finish"))))
		}
	}

	return edges
}

// switchTimelineKey is the key that leaves an entry to pick the timeline it adds to.
func switchTimelineKey() key.Binding {
	return key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "switch"))
}

// keyHint returns b's help as a huh group's help row draws it.
func keyHint(b key.Binding) string {
	styles := help.New().Styles
	return styles.ShortKey.Render(b.Help().Key) + " " + styles.ShortDesc.Render(b.Help().Desc)
}

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

// newForm returns the form, framed with edges, that asks groups in turn.
func newForm(edges frameEdges, groups ...formGroup) *fittedForm {
	pages := make([]*huh.Group, len(groups))
	for i, g := range groups {
		pages[i] = huh.NewGroup(g.fields...)
		if g.hide != nil {
			pages[i] = pages[i].WithHideFunc(g.hide)
		}
	}

	return &fittedForm{Form: huh.NewForm(pages...), edges: edges}
}

// formWidth is the widest a form's frame grows, so a wide terminal shows it as a card.
const formWidth = 80

// fittedForm is a form drawn in a frame headed by its title, a blank row above it to set it apart
// from the output above. It tells huh its terminal is the inside of the frame, less that row, and a
// row shorter still, since huh sizes a group's help as one row but draws a blank row above it, and
// the terminal drops a taller frame's top row.
type fittedForm struct {
	*huh.Form
	edges frameEdges
	// width is the frame's, which the last window size sets.
	width int
}

func (f *fittedForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		f.width = min(size.Width, formWidth)
		size.Width = f.width - frameBox().GetHorizontalFrameSize()
		// The blank row above the frame, the top edge frame draws, then huh's blank row.
		size.Height -= frameBox().GetVerticalFrameSize() + 3
		msg = size
	}
	_, cmd := f.Form.Update(msg)

	return f, cmd
}

func (f *fittedForm) View() tea.View {
	body := f.Form.View()
	// huh draws nothing once the form ends, which clears it from the screen.
	if body != "" {
		body = "\n" + frame(f.edges, body, f.width)
	}
	v := tea.NewView(body)
	v.ReportFocus = true

	return v
}

// frameBox is a form frame's sides and bottom edge, with a blank row under the top edge frame draws.
func frameBox() lipgloss.Style {
	return lipgloss.NewStyle().Padding(1, 1, 0).Border(lipgloss.RoundedBorder(), false, true, true).
		BorderForeground(lipgloss.BrightBlack)
}

// frameEdges is what a frame sets into its edges: title at the top edge's left, and the pieces of
// top and bottom at the right of those edges.
type frameEdges struct {
	title       string
	top, bottom []string
}

// frame returns body in a rounded frame width cells wide, clipping a wider line, or as wide as body
// when width is 0, with edges set into its edges.
func frame(edges frameEdges, body string, width int) string {
	inside := lipgloss.NewStyle()
	if width > 0 {
		// huh can draw a line wider than it was told, as its help does; wrapping it would add a row
		// the form's height never planned for.
		inside = inside.MaxWidth(width - frameBox().GetHorizontalFrameSize())
	}
	box := frameBox().Width(width).Render(inside.Render(body))
	boxWidth := lipgloss.Width(box)
	top := lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Render("╭─ ") + lipgloss.NewStyle().Bold(true).Render(edges.title) +
		edgeEnd(" ", edges.top, boxWidth-ansi.StringWidth("╭─ "+edges.title), "╮")
	if len(edges.bottom) > 0 {
		rows := strings.Split(box, "\n")
		rows[len(rows)-1] = edgeEnd("╰", edges.bottom, boxWidth, "╯")
		box = strings.Join(rows, "\n")
	}

	return top + "\n" + box
}

// edgeEnd returns the rest of a frame's edge, width cells wide: lead, a rule, pieces set off by
// spaces, and corner. Where the edge is too narrow for them and a rule cell, it drops the last
// pieces, then cuts the first short.
func edgeEnd(lead string, pieces []string, width int, corner string) string {
	edge := lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	rule := width - ansi.StringWidth(lead+corner)
	// A rule cell, a space each side of the pieces, and the rule cell before the corner.
	right := fitted(pieces, rule-4)
	if right == "" {
		return edge.Render(lead + strings.Repeat("─", max(0, rule)) + corner)
	}

	return edge.Render(lead+strings.Repeat("─", rule-ansi.StringWidth(right)-3)+" ") + right + edge.Render(" ─"+corner)
}

// fitted returns pieces two spaces apart in at most width cells: the last ones dropped, then the
// first cut short, until they fit.
func fitted(pieces []string, width int) string {
	for len(pieces) > 1 && ansi.StringWidth(strings.Join(pieces, "  ")) > width {
		pieces = pieces[:len(pieces)-1]
	}

	return ansi.Truncate(strings.Join(pieces, "  "), max(0, width), "…")
}

// errAskAgain is what a form's submission returns to ask the form again without printing an error.
var errAskAgain = errors.New("ask the form again")

// errSwitchTimeline is what an entry's ask returns when ctrl+t leaves it to pick another timeline.
var errSwitchTimeline = errors.New("switch the timeline")

// errEntryCancelled is what an entry returns when Esc leaves it, adding nothing.
var errEntryCancelled = errors.New("entry cancelled")

// formSession is what one run of a write command's form reads and writes.
type formSession struct {
	app App
	inv invocation
	ask askFunc
	// frame is what the session's next ask shows beside its form.
	frame formFrame
	// thumbnails are what the form's list fields show under their values, by field name.
	thumbnails map[string]func(value string) (string, tea.Cmd)
	// completions are what the form's list fields complete typed text to, by field name.
	completions map[string]func(typed string) []string
	// paths turn what is typed into the form's list fields into their values, by field name.
	paths map[string]func(typed string) string
	// placeholders are what the form's empty Inputs show as their default, by field name.
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
	// An invalid default author has no placeholder; submitting without an author reports it.
	author, err := defaultAuthor(nil, inv.cfg.Author, app.LookupEnv)
	if err == nil && author != nil {
		placeholders["author"] = *author
	}

	home, _ := app.LookupEnv("HOME")

	return formSession{
		app: app, inv: inv, ask: forms(inv.outColor, images),
		thumbnails:   map[string]func(string) (string, tea.Cmd){"attach": attachThumbnail(app.ReadFile, images)},
		completions:  map[string]func(string) []string{"attach": attachCompletions(app.ReadDir, home)},
		paths:        map[string]func(string) string{"attach": expandHome(home)},
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

// session asks for entries, prefilled from in, adding each one submitted, until no other is wanted,
// then summarizes what it added.
func (f addForm[R]) session(ctx context.Context, s formSession, in fieldValues) error {
	// No form field asks the id, so no form could correct a bad one.
	err := onlyField(f.def, "id").check(in, baseCheckEnv(s))
	if err != nil {
		return err
	}
	picker := timelinePicker{def: f.def, create: newTimeline}
	in, chosen, err := chooseTimeline(ctx, s, picker, in)
	if err != nil || !chosen {
		return err
	}

	noun, _, _ := strings.Cut(f.path, " ")
	var added []summaryLine
	for {
		current, err := selectedTimeline(ctx, s, in)
		if err != nil {
			return err
		}
		s.frame = formFrame{timeline: current.Name, entry: true, added: len(added), noun: noun}
		typed, entered, err := f.entry(ctx, s, in)
		added = append(added, entered...)
		switch {
		case errors.Is(err, errSwitchTimeline):
			s.frame = formFrame{added: len(added), noun: noun}
			picker.current = current.Name
			in, err = switchTimeline(ctx, s, picker, in, typed)
			if err != nil {
				return err
			}
			continue
		case errors.Is(err, errEntryCancelled):
			return s.summarize(noun, added)
		case err != nil:
			return err
		}

		s.frame = formFrame{timeline: current.Name, added: len(added), noun: noun}
		another, err := s.askAnother(ctx, noun)
		if err != nil {
			return err
		}
		if !another {
			return s.summarize(noun, added)
		}

		in = carryForward(typed, f.def)
	}
}

// askAnother asks whether to add another noun record, and reports whether so.
func (s formSession) askAnother(ctx context.Context, noun string) (bool, error) {
	yes := true
	another := huh.NewConfirm().Title("➕ Add another " + noun + "?").Value(&yes)
	submitted, err := s.ask(ctx, s.frame, formGroup{fields: []huh.Field{another}})
	if err != nil {
		return false, askError(err)
	}
	// Value can rebind the confirm, so read what it holds.
	accepted, _ := another.GetValue().(bool)

	return submitted && accepted, nil
}

// summarize prints, for each timeline the noun records added lists went to, in the order each first
// got one, a heading counting them and a row for each, then the command that views those timelines;
// nothing when added is empty.
func (s formSession) summarize(noun string, added []summaryLine) error {
	if len(added) == 0 {
		return nil
	}
	styles := newNoteStyles(s.inv.errColor)
	groups := byTimeline(added)
	// One table, so the columns line up across the timelines.
	rows := strings.Split(summaryRows(slices.Concat(groups...), styles.faint), "\n")
	blocks := make([]string, len(groups))
	view := slices.Concat([]string{"coroner"}, s.inv.echoArgs, []string{"timeline", "view"})
	for i, group := range groups {
		name := group[0].timeline
		blocks[i] = fmt.Sprintf("%s Added %d %s to timeline %s\n%s", styles.done.Render("✓"), len(group),
			pluralNoun(len(group), noun), styles.name.Render(name), strings.Join(rows[:len(group)], "\n"))
		rows = rows[len(group):]
		view = append(view, "-t", name)
	}
	label := "View the timeline:"
	if len(groups) > 1 {
		label = "View the timelines:"
	}

	return s.inv.out.notify("\n%s\n\n  %s %s", strings.Join(blocks, "\n"), styles.faint.Render(label), shellLine(view))
}

// byTimeline returns added grouped by timeline, in the order each timeline first appears.
func byTimeline(added []summaryLine) [][]summaryLine {
	var groups [][]summaryLine
	for _, line := range added {
		i := slices.IndexFunc(groups, func(group []summaryLine) bool { return group[0].timeline == line.timeline })
		if i == -1 {
			groups = append(groups, nil)
			i = len(groups) - 1
		}
		groups[i] = append(groups[i], line)
	}

	return groups
}

// summaryRows returns added's rows, two spaces in, with each cell before the text drawn in lead and
// padded to its column's widest, so the texts line up.
func summaryRows(added []summaryLine, lead lipgloss.Style) string {
	widths := make([]int, len(added[0].cells)-1)
	for _, line := range added {
		for i := range widths {
			widths[i] = max(widths[i], ansi.StringWidth(line.cells[i]))
		}
	}
	rows := make([]string, 0, len(added))
	for _, line := range added {
		var row strings.Builder
		row.WriteString("  ")
		for i, width := range widths {
			row.WriteString(lead.Render(line.cells[i]) + strings.Repeat(" ", width-ansi.StringWidth(line.cells[i])+2))
		}
		rows = append(rows, row.String()+line.cells[len(widths)])
	}

	return strings.Join(rows, "\n")
}

// noteStyles are how a form session's notes draw in the terminal's own colors: faint for what
// supports a line, and a green mark and bold name for the summary's heading. Without color they are
// plain, with the name in backticks.
type noteStyles struct{ faint, done, name lipgloss.Style }

func newNoteStyles(color bool) noteStyles {
	s := lipgloss.NewStyle()
	if !color {
		return noteStyles{faint: s, done: s, name: s.Transform(func(name string) string { return "`" + name + "`" })}
	}

	return noteStyles{faint: s.Faint(true), done: s.Foreground(lipgloss.Green), name: s.Bold(true)}
}

// chooseTimeline returns in with the timeline the session writes to, asking for one through picker
// when no layer selects it, and whether there is one: Esc on that ask leaves none.
func chooseTimeline(ctx context.Context, s formSession, picker timelinePicker, in fieldValues) (fieldValues, bool, error) {
	var by selectedBy
	err := withStorePath(ctx, s.app, s.inv.cfg.DBPath, func(st *store.Store) error {
		sel, err := selectTimelines(ctx, st, timelineInputOf(in), s.app.LookupEnv)
		if err != nil || sel.by != "" {
			by = sel.by
			return err
		}

		picker.timelines, err = st.Timelines(ctx)
		if err == nil && len(picker.timelines) == 0 {
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

	name, picked, err := picker.ask(ctx, s)
	if err != nil || !picked {
		return in, false, err
	}
	chosen := maps.Clone(in)
	chosen["timeline"] = []string{name}

	return chosen, true, nil
}

// timelinePicker asks for a timeline through def's timeline field: one of timelines, opening on
// current, else the first, or, when create is set, ✨ New timeline, which create asks for.
type timelinePicker struct {
	def       definition
	timelines []timeline.Timeline
	current   string
	// create returns the name of the timeline it created or used, and whether there is one.
	create func(context.Context, formSession) (string, bool, error)
}

// ask returns the name of the timeline picked, and whether there is one: Esc leaves none.
func (p timelinePicker) ask(ctx context.Context, s formSession) (string, bool, error) {
	var options []huh.Option[string]
	if p.create != nil {
		// An empty value is no timeline, which the field's check never rejects.
		options = append(options, huh.NewOption("✨ New timeline", ""))
	}
	for _, t := range p.timelines {
		options = append(options, huh.NewOption(strings.TrimSpace(t.Name+"  "+pickerLabel(t.Title)), t.Name))
	}
	pick := onlyField(p.def, "timeline")
	opening := fieldValues{"timeline": {cmp.Or(p.current, p.timelines[0].Name)}}
	fields := pick.formFields(baseCheckEnv(s), opening, formChoices{options: map[string][]huh.Option[string]{"timeline": options}})

	submitted, err := s.ask(ctx, s.frame, formGroup{fields: fields})
	switch {
	case err != nil:
		return "", false, askError(err)
	case !submitted:
		return "", false, nil
	}
	name := pick.formValues(fields).value("timeline")
	if name == nil {
		return p.create(ctx, s)
	}

	return *name, true, nil
}

// switchTimeline asks through picker which timeline the entry typed goes to, and returns what the
// entry reopens with: typed, with the timeline and id in gives, which no entry asks, on the timeline
// picked or created, if any.
func switchTimeline(ctx context.Context, s formSession, picker timelinePicker, in, typed fieldValues) (fieldValues, error) {
	reopened := maps.Clone(typed)
	for _, name := range []string{"timeline", "id"} {
		if in.given(name) {
			reopened[name] = in.values(name)
		}
	}
	var err error
	picker.timelines, err = storedTimelines(ctx, s)
	if err != nil {
		return reopened, err
	}
	name, picked, err := picker.ask(ctx, s)
	if picked {
		reopened["timeline"] = []string{name}
	}

	return reopened, err
}

// storedTimelines returns the timelines in the session's database.
func storedTimelines(ctx context.Context, s formSession) ([]timeline.Timeline, error) {
	var timelines []timeline.Timeline
	err := withStorePath(ctx, s.app, s.inv.cfg.DBPath, func(st *store.Store) error {
		var err error
		timelines, err = st.Timelines(ctx)
		return err
	})

	return timelines, err
}

// newTimeline asks for a timeline through timeline create's own form, framed as s frames its asks
// under that form's title, and returns its name, and whether one was created or used.
func newTimeline(ctx context.Context, s formSession) (string, bool, error) {
	s.frame.title = timelineCreateTitle
	t, created, err := askTimelineCreate(ctx, s, timelineCreateWrite(), fieldValues{})

	return t.Name, created, err
}

// selectedTimeline returns the timeline in selects, which an entry prefilled from in adds to.
func selectedTimeline(ctx context.Context, s formSession, in fieldValues) (timeline.Timeline, error) {
	var t timeline.Timeline
	err := withStorePath(ctx, s.app, s.inv.cfg.DBPath, func(st *store.Store) error {
		var err error
		t, err = selectOne(ctx, st, timelineInputOf(in), s.app.LookupEnv)
		return err
	})

	return t, err
}

// entry asks for one entry, prefilled from in, and adds it to in's timeline once submitted. It returns
// the values last submitted, or in when none was, and the summary's line for the record it stored;
// errEntryCancelled when Esc leaves it, and what was typed with errSwitchTimeline when ctrl+t does.
func (f addForm[R]) entry(ctx context.Context, s formSession, in fieldValues) (fieldValues, []summaryLine, error) {
	choices, err := f.entryChoices(ctx, s, in, baseCheckEnv(s).zone)
	if err != nil {
		return in, nil, err
	}

	// The picker chose the timeline, and an id only a flag gives applies to the first entry.
	def := withoutField(withoutField(f.def, "timeline"), "id")
	if i := def.fieldIndex("source"); i != -1 {
		// sourceChoices suggests every stored source.
		def.fields[i].preview = storedPreview("source", checkSource, choices.suggestions["source"])
	}
	if i := def.fieldIndex("tag"); i != -1 {
		// eventChoices suggests every stored tag.
		def.fields[i].preview = storedPreview("tag", checkTag, choices.suggestions["tag"])
	}

	var added []summaryLine
	typed, submitted, err := s.askUntil(ctx, def, choices, in, func(typed fieldValues) error {
		if in.given("timeline") {
			typed["timeline"] = in.values("timeline")
		}
		if in.given("id") {
			typed["id"] = in.values("id")
		}
		result, err := f.submit(ctx, s, typed)
		if err != nil {
			return err
		}
		if line, stored := result.summary(); stored {
			added = append(added, line)
		}
		return nil
	})
	if err == nil && !submitted {
		err = errEntryCancelled
	}

	return typed, added, err
}

// askUntil asks def's fields, prefilled from in and picking from choices, and passes what was typed to
// submit. It asks again with what was typed while submit returns errAskAgain, or a correctable error,
// which it prints. It returns what was last typed, whether submit took it, and submit's error; in when
// Esc ends the form, or what was typed and errSwitchTimeline when ctrl+t does.
func (s formSession) askUntil(
	ctx context.Context, def definition, choices formChoices, in fieldValues, submit func(typed fieldValues) error,
) (fieldValues, bool, error) {
	choices.thumbnails = s.thumbnails
	choices.completions = s.completions
	choices.paths = s.paths
	choices.placeholders = s.placeholders
	for {
		fields := def.formFields(baseCheckEnv(s), in, choices)
		groups := def.formGroups(fields, in)
		submitted, err := s.ask(ctx, s.frame, groups...)
		typed := def.formValues(def.shownFields(groups))
		switch {
		case errors.Is(err, errSwitchTimeline):
			return typed, false, err
		case err != nil:
			return in, false, askError(err)
		case !submitted:
			return in, false, nil
		}

		err = submit(typed)
		rejected, _ := errors.AsType[*Error](err)
		switch {
		case errors.Is(err, errAskAgain):
		case correctable(err):
			// A rejected entry is reported to the person at the form, never as a JSON envelope.
			err = writeError(s.app.Err, rejected, errorStyle{color: s.inv.errColor})
			if err != nil {
				return typed, false, err
			}
		default:
			return typed, err == nil, err
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
// equals, against the same database. It returns the result.
func (w formWrite[R]) submit(ctx context.Context, s formSession, values fieldValues) (R, error) {
	result, err := w.run(ctx, s.app, s.inv.cfg, values)
	if err != nil {
		return result, err
	}
	err = result.render(s.inv.out)
	if err != nil {
		return result, err
	}

	return result, s.echo(w.path, w.def, values)
}

// echo prints the flags-only command that gives values to the command at path, with def's
// fields, against the same database.
func (s formSession) echo(path string, def definition, values fieldValues) error {
	args := commandArgs(path, def, values)
	label := newNoteStyles(s.inv.errColor).faint.Render("Equivalent command:")

	return s.inv.out.notify("%s %s", label, shellLine(slices.Concat(args[:1], s.inv.echoArgs, args[1:])))
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

// terminalForms returns the askFunc that runs forms framed under title on app's terminal, drawing in
// color when color is true, in the profile an attach field needs for the images the terminal shows.
func terminalForms(app App, title string) func(color bool, images graphics.Protocol) askFunc {
	return func(color bool, images graphics.Protocol) askFunc {
		return func(ctx context.Context, f formFrame, groups ...formGroup) (bool, error) {
			fields := fieldsOf(groups)
			attach := slices.ContainsFunc(fields, func(f huh.Field) bool { return f.GetKey() == "attach" })
			profile := formProfile(color, images, attach)
			var keys formKeys
			form := newForm(f.edges(title), groups...)
			// Not tea.Interrupt for cancel: bubbletea skips an interrupted program's last frame, the
			// empty one that clears the form.
			form.SubmitCmd, form.CancelCmd = tea.Quit, tea.Quit
			// The caller's context already ends on a signal, and the profile is pinned so bubbletea
			// never reads the process environment to pick one.
			_, err := tea.NewProgram(form, tea.WithContext(ctx), tea.WithInput(app.In), tea.WithOutput(app.Out),
				tea.WithFilter(keys.filter(f, fields)), tea.WithColorProfile(profile), tea.WithoutSignalHandler()).Run()

			return keys.ended(form.State, err)
		}
	}
}

// formKeys records which key the program filter it returns turned into a form's own abort.
type formKeys struct{ cancelled, switched bool }

// filter returns the program filter of a form framed as f, of fields: escCancels, and on an entry, one
// that turns ctrl+t into the form's own abort too, so no field sees it.
func (k *formKeys) filter(f formFrame, fields []huh.Field) func(tea.Model, tea.Msg) tea.Msg {
	esc := escCancels(&k.cancelled, fields)
	if !f.entry {
		return esc
	}

	return func(m tea.Model, msg tea.Msg) tea.Msg {
		press, ok := msg.(tea.KeyPressMsg)
		if !ok || !key.Matches(press, switchTimelineKey()) {
			return esc(m, msg)
		}
		k.switched = true
		// ctrl+c is huh's Quit binding.
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
}

// ended returns what an ask returns for its form, which ended in state with err.
func (k *formKeys) ended(state huh.FormState, err error) (bool, error) {
	switch {
	case k.switched:
		return false, errSwitchTimeline
	case k.cancelled:
		return false, nil
	case state == huh.StateAborted:
		return false, huh.ErrUserAborted
	}

	return err == nil, err
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
		choices: eventChoices,
	}
}

// eventChoices suggests sources as sourceChoices does, and t's tags, ordered by their latest event's
// occurred at, latest first, then every other tag there is.
func eventChoices(ctx context.Context, s *store.Store, t timeline.Timeline, _ zone) (formChoices, error) {
	choices, err := sourceChoices(ctx, s, t)
	if err != nil {
		return formChoices{}, err
	}
	timelineTags, err := s.TagUsages(ctx, t.ID)
	if err != nil {
		return formChoices{}, err
	}
	everyTag, err := s.TagUsages(ctx)
	if err != nil {
		return formChoices{}, err
	}
	choices.suggestions["tag"] = appendUnique(tagNames(timelineTags), tagNames(everyTag)...)

	return choices, nil
}

func tagNames(usages []store.TagUsage) []string {
	names := make([]string, 0, len(usages))
	for _, usage := range usages {
		names = append(names, usage.Name)
	}
	return names
}

func annotationAddForm() addForm[annotationAdded] {
	return addForm[annotationAdded]{
		path: "annotation add", def: annotationAddDefinition(), run: addAnnotation,
		choices: annotationChoices,
	}
}

// sourceChoices suggests t's sources for the source field, the one on its latest event first, then
// every other source there is.
func sourceChoices(ctx context.Context, s *store.Store, t timeline.Timeline) (formChoices, error) {
	timelineSources, err := s.TimelineSources(ctx, t.ID)
	if err != nil {
		return formChoices{}, err
	}
	everySource, err := s.Sources(ctx)
	if err != nil {
		return formChoices{}, err
	}
	names := appendUnique(sourceNames(timelineSources), sourceNames(everySource)...)

	return formChoices{suggestions: map[string][]string{"source": names}}, nil
}

// storedPreview returns the preview of a field naming a noun: the hint of the check a typed value
// fails, else whether it is one of existing or a new noun.
func storedPreview(noun string, check func(string) error, existing []string) func(string, checkEnv) string {
	return func(value string, _ checkEnv) string {
		rejected, ok := errors.AsType[*Error](check(value))
		switch {
		case value == "":
			return ""
		case ok:
			return rejected.Hint
		case slices.Contains(existing, value):
			return "✅ Existing " + noun
		}

		return newMark + noun
	}
}

// newMark starts the preview of a value no stored record has; a list field marks such held values.
const newMark = "✨ New "

func sourceNames(sources []timeline.Source) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.Name)
	}
	return names
}

// annotationChoices suggests sources, picks the kind, and picks the event from t's events, or none
// for the whole timeline.
func annotationChoices(ctx context.Context, s *store.Store, t timeline.Timeline, z zone) (formChoices, error) {
	choices, err := sourceChoices(ctx, s, t)
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
