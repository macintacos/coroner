// Package clikit holds what coroner's commands share: the App they run against, errors and exit
// codes, output, timeline selection, write-command definitions and their forms, and logging.
package clikit

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
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

// FormWrite is a write command a form submits to: its path below coroner, definition and run function.
type FormWrite[R writeResult] struct {
	// Path is the command below coroner, as in "event add".
	Path string
	Def  Definition
	Run  func(context.Context, App, config.Config, FieldValues) (R, error)
}

// AddForm is an add command's form: the command it submits to, and what each entry picks from.
type AddForm[R addResult] struct {
	FormWrite[R]
	// Choices reads what the form picks from once the timeline t is known, labelling times in z.
	Choices     func(ctx context.Context, s *store.Store, t timeline.Timeline, z Zone) (FormChoices, error)
	NewTimeline TimelineAsk
}

// TimelineAsk asks for a timeline to create, and returns the name of the one it created or used,
// and whether there is one.
type TimelineAsk func(context.Context, FormSession) (string, bool, error)

// addResult is what an add form's command returns for an entry: rendered once submitted, and listed
// in the summary that ends the session when the add stored a record.
type addResult interface {
	renderer
	changeLogger
	// Summary returns how the session's summary lists the record, and whether the add stored it.
	Summary() (SummaryLine, bool)
}

// SummaryLine is a record as the summary that ends an add form's session lists it: its timeline's
// name, and the cells of its row under the count, its text last.
type SummaryLine struct {
	Timeline string
	Cells    []string
}

// AskFunc runs a form of groups in frame and reports whether it was submitted: Esc returns false,
// Ctrl-C huh.ErrUserAborted.
type AskFunc func(ctx context.Context, frame FormFrame, groups ...FormGroup) (bool, error)

// FormFrame is what an ask's frame shows beside the form: a title in place of the command's, and an
// add session's timeline and the noun records it added, in the frame's edges.
type FormFrame struct {
	Title    string
	Timeline string
	// Entry is whether the form is an entry, which ctrl+t and Esc leave.
	Entry bool
	Added int
	Noun  string
}

// Edges returns what f's frame sets into its edges, titled title unless f names another: the
// timeline, and the count of records added once there is one, each with the key that leaves an
// entry for it.
func (f FormFrame) Edges(title string) FrameEdges {
	edges := FrameEdges{Title: cmp.Or(f.Title, title)}
	if f.Timeline != "" {
		edges.Top = []string{"📒 " + f.Timeline}
		if f.Entry {
			edges.Top = append(edges.Top, keyHint(switchTimelineKey()))
		}
	}
	if f.Added > 0 {
		done := lipgloss.NewStyle().Foreground(lipgloss.Green).Render("✓")
		edges.Bottom = []string{done + " " + strconv.Itoa(f.Added) + " " + PluralNoun(f.Added, f.Noun) + " added"}
		if f.Entry {
			edges.Bottom = append(edges.Bottom, keyHint(key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "finish"))))
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

// FormGroup is one page of a form: its fields, which the form skips while hide reports true.
type FormGroup struct {
	Fields []huh.Field
	// Hide reports whether the form skips the group; nil never does.
	Hide func() bool
}

// FieldsOf returns every field of groups, in order.
func FieldsOf(groups []FormGroup) []huh.Field {
	var fields []huh.Field
	for _, g := range groups {
		fields = append(fields, g.Fields...)
	}

	return fields
}

// NewForm returns the form, framed with edges, that asks groups in turn.
func NewForm(edges FrameEdges, groups ...FormGroup) *FittedForm {
	pages := make([]*huh.Group, len(groups))
	for i, g := range groups {
		pages[i] = huh.NewGroup(g.Fields...)
		if g.Hide != nil {
			pages[i] = pages[i].WithHideFunc(g.Hide)
		}
	}

	return &FittedForm{Form: huh.NewForm(pages...), edges: edges}
}

// formWidth is the widest a form's frame grows, so a wide terminal shows it as a card.
const formWidth = 80

// FittedForm is a form drawn in a frame headed by its title, a blank row above it to set it apart
// from the output above. It tells huh its terminal is the inside of the frame, less that row, and a
// row shorter still, since huh sizes a group's help as one row but draws a blank row above it, and
// the terminal drops a taller frame's top row.
type FittedForm struct {
	*huh.Form
	edges FrameEdges
	// width is the frame's, which the last window size sets.
	width int
}

// Update fits the form to the terminal's width and passes msg on to it.
func (f *FittedForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

// View draws the form inside its frame.
func (f *FittedForm) View() tea.View {
	body := f.Form.View()
	// huh draws nothing once the form ends, which clears it from the screen.
	if body != "" {
		body = "\n" + Frame(f.edges, body, f.width)
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

// FrameEdges is what a frame sets into its edges: title at the top edge's left, and the pieces of
// top and bottom at the right of those edges.
type FrameEdges struct {
	Title       string
	Top, Bottom []string
}

// Frame returns body in a rounded frame width cells wide, clipping a wider line, or as wide as body
// when width is 0, with edges set into its edges.
func Frame(edges FrameEdges, body string, width int) string {
	inside := lipgloss.NewStyle()
	if width > 0 {
		// huh can draw a line wider than it was told, as its help does; wrapping it would add a row
		// the form's height never planned for.
		inside = inside.MaxWidth(width - frameBox().GetHorizontalFrameSize())
	}
	box := frameBox().Width(width).Render(inside.Render(body))
	boxWidth := lipgloss.Width(box)
	top := lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Render("╭─ ") + lipgloss.NewStyle().Bold(true).Render(edges.Title) +
		edgeEnd(" ", edges.Top, boxWidth-ansi.StringWidth("╭─ "+edges.Title), "╮")
	if len(edges.Bottom) > 0 {
		rows := strings.Split(box, "\n")
		rows[len(rows)-1] = edgeEnd("╰", edges.Bottom, boxWidth, "╯")
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

// ErrAskAgain is what a form's submission returns to ask the form again without printing an error.
var ErrAskAgain = errors.New("ask the form again")

// ErrSwitchTimeline is what an entry's ask returns when ctrl+t leaves it to pick another timeline.
var ErrSwitchTimeline = errors.New("switch the timeline")

// errEntryCancelled is what an entry returns when Esc leaves it, adding nothing.
var errEntryCancelled = errors.New("entry cancelled")

// FormSession is what one run of a write command's form reads and writes.
type FormSession struct {
	App App
	Inv Invocation
	Ask AskFunc
	// Frame is what the session's next ask shows beside its form.
	Frame FormFrame
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

// FormCommand is a write command with a form.
type FormCommand struct {
	Def Definition
	// Ready reports whether the command line's values run without a form.
	Ready func(in FieldValues) bool
	// FlagsOnly runs the command line without a form.
	FlagsOnly InvokedRunE
	// Session runs the form, prefilled from the command line's values.
	Session func(s FormSession, in FieldValues) error
}

// Attach registers c's flags and --interactive on cmd, and a RunE that asks through forms when
// FormMode opens the form, and otherwise runs flagsOnly.
func (c FormCommand) Attach(cmd *cobra.Command, app App, forms func(color bool, images graphics.Protocol) AskFunc) *cobra.Command {
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		inv, err := ReadInvocation(cmd, app)
		if err != nil {
			return err
		}
		interactive, _ := cmd.Flags().GetBool("interactive")
		if interactive && !inv.Terminal {
			return interactiveRefusal(cmd, inv.InputOff)
		}
		in := c.Def.Read(cmd.Flags(), args)
		if !FormMode(FormGate{Interactive: interactive, Ready: c.Ready(in), Machine: inv.Out.Format.Machine(), Terminal: inv.Terminal}) {
			return c.FlagsOnly(cmd, inv, args)
		}

		return c.Session(newFormSession(app, inv, forms), in)
	}

	c.Def.AddFlags(cmd.Flags())
	cmd.Flags().Bool("interactive", false, interactiveUsage)

	return cmd
}

// Attach registers f's flags and --interactive on cmd, and a RunE that asks through forms when a
// field the command requires is missing or --interactive asks, and otherwise runs WriteRunE.
func (f AddForm[R]) Attach(
	ctx context.Context, cmd *cobra.Command, app App, forms func(color bool, images graphics.Protocol) AskFunc,
) *cobra.Command {
	return FormCommand{
		Def:       f.Def,
		Ready:     func(in FieldValues) bool { return RequiredGiven(f.Def, in) },
		FlagsOnly: WriteRun(ctx, app, f.Def, f.Run),
		Session: func(s FormSession, in FieldValues) error {
			return f.session(ctx, s, in)
		},
	}.Attach(cmd, app, forms)
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

// InlineImages is how a form shows images on app's terminal under inv, logged at
// debug: none without color, since Kitty graphics name an image by the color of the cells it
// shows through.
func InlineImages(app App, inv Invocation) graphics.Protocol {
	images := graphics.None
	if inv.OutColor {
		images = graphics.Detect(app.LookupEnv)
	}
	app.Logger().Debug("inline images decided", "protocol", images.String())

	return images
}

// newFormSession returns the session a form runs in under inv, asking through forms.
func newFormSession(app App, inv Invocation, forms func(color bool, images graphics.Protocol) AskFunc) FormSession {
	images := InlineImages(app, inv)

	placeholders := map[string]string{}
	// An invalid default author has no placeholder; submitting without an author reports it.
	author, err := DefaultAuthor(nil, inv.Cfg.Author, app.LookupEnv)
	if err == nil && author != nil {
		placeholders["author"] = *author
	}

	home, _ := app.LookupEnv("HOME")

	return FormSession{
		App: app, Inv: inv, Ask: forms(inv.OutColor, images),
		thumbnails:   map[string]func(string) (string, tea.Cmd){"attach": AttachThumbnail(app.ReadFile, images)},
		completions:  map[string]func(string) []string{"attach": AttachCompletions(app.ReadDir, home)},
		paths:        map[string]func(string) string{"attach": ExpandHome(home)},
		placeholders: placeholders,
	}
}

// FormGate is what FormMode decides from: whether --interactive asks for the form, the command
// line's values run without it, a machine format was asked for, and a form may take over the
// terminal.
type FormGate struct {
	Interactive, Ready, Machine, Terminal bool
}

// FormMode reports whether a write command opens its form: on a terminal, when --interactive asks
// for it, or when the command line is not ready to run without it and no machine format was asked
// for, since a machine format's caller is a script.
func FormMode(g FormGate) bool {
	return g.Terminal && (g.Interactive || !g.Ready && !g.Machine)
}

// RequiredGiven reports whether in gives every field d requires.
func RequiredGiven(d Definition, in FieldValues) bool {
	return !slices.ContainsFunc(d.Fields, func(f Field) bool { return f.Required && !in.Given(f.Name) })
}

// session asks for entries, prefilled from in, adding each one submitted, until no other is wanted,
// then summarizes what it added.
func (f AddForm[R]) session(ctx context.Context, s FormSession, in FieldValues) error {
	// No form field asks the id, so no form could correct a bad one.
	err := OnlyField(f.Def, "id").Check(in, BaseCheckEnv(s))
	if err != nil {
		return err
	}
	picker := TimelinePicker{Def: f.Def, create: f.NewTimeline}
	in, chosen, err := ChooseTimeline(ctx, s, picker, in)
	if err != nil || !chosen {
		return err
	}

	noun, _, _ := strings.Cut(f.Path, " ")
	var added []SummaryLine
	for {
		current, err := selectedTimeline(ctx, s, in)
		if err != nil {
			return err
		}
		s.Frame = FormFrame{Timeline: current.Name, Entry: true, Added: len(added), Noun: noun}
		typed, entered, err := f.entry(ctx, s, in)
		added = append(added, entered...)
		switch {
		case errors.Is(err, ErrSwitchTimeline):
			s.Frame = FormFrame{Added: len(added), Noun: noun}
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

		s.Frame = FormFrame{Timeline: current.Name, Added: len(added), Noun: noun}
		another, err := s.askAnother(ctx, noun)
		if err != nil {
			return err
		}
		if !another {
			return s.summarize(noun, added)
		}

		in = CarryForward(typed, f.Def)
	}
}

// askAnother asks whether to add another noun record, and reports whether so.
func (s FormSession) askAnother(ctx context.Context, noun string) (bool, error) {
	yes := true
	another := huh.NewConfirm().Title("➕ Add another " + noun + "?").Value(&yes)
	submitted, err := s.Ask(ctx, s.Frame, FormGroup{Fields: []huh.Field{another}})
	if err != nil {
		return false, AskError(err)
	}
	// Value can rebind the confirm, so read what it holds.
	accepted, _ := another.GetValue().(bool)

	return submitted && accepted, nil
}

// summarize prints, for each timeline the noun records added lists went to, in the order each first
// got one, a heading counting them and a row for each, then the command that views those timelines;
// nothing when added is empty.
func (s FormSession) summarize(noun string, added []SummaryLine) error {
	if len(added) == 0 {
		return nil
	}
	styles := newNoteStyles(s.Inv.ErrColor)
	groups := byTimeline(added)
	// One table, so the columns line up across the timelines.
	rows := strings.Split(SummaryRows(slices.Concat(groups...), styles.faint), "\n")
	blocks := make([]string, len(groups))
	view := slices.Concat([]string{"coroner"}, s.Inv.EchoArgs, []string{"timeline", "view"})
	for i, group := range groups {
		name := group[0].Timeline
		blocks[i] = fmt.Sprintf("%s Added %d %s to timeline %s\n%s", styles.done.Render("✓"), len(group),
			PluralNoun(len(group), noun), styles.name.Render(name), strings.Join(rows[:len(group)], "\n"))
		rows = rows[len(group):]
		view = append(view, "-t", name)
	}
	label := "View the timeline:"
	if len(groups) > 1 {
		label = "View the timelines:"
	}

	return s.Inv.Out.Notify("\n%s\n\n  %s %s", strings.Join(blocks, "\n"), styles.faint.Render(label), ShellLine(view))
}

// byTimeline returns added grouped by timeline, in the order each timeline first appears.
func byTimeline(added []SummaryLine) [][]SummaryLine {
	var groups [][]SummaryLine
	for _, line := range added {
		i := slices.IndexFunc(groups, func(group []SummaryLine) bool { return group[0].Timeline == line.Timeline })
		if i == -1 {
			groups = append(groups, nil)
			i = len(groups) - 1
		}
		groups[i] = append(groups[i], line)
	}

	return groups
}

// SummaryRows returns added's rows, two spaces in, with each cell before the text drawn in lead and
// padded to its column's widest, so the texts line up.
func SummaryRows(added []SummaryLine, lead lipgloss.Style) string {
	widths := make([]int, len(added[0].Cells)-1)
	for _, line := range added {
		for i := range widths {
			widths[i] = max(widths[i], ansi.StringWidth(line.Cells[i]))
		}
	}
	rows := make([]string, 0, len(added))
	for _, line := range added {
		var row strings.Builder
		row.WriteString("  ")
		for i, width := range widths {
			row.WriteString(lead.Render(line.Cells[i]) + strings.Repeat(" ", width-ansi.StringWidth(line.Cells[i])+2))
		}
		rows = append(rows, row.String()+line.Cells[len(widths)])
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

// ChooseTimeline returns in with the timeline the session writes to, asking for one through picker
// when no layer selects it, and whether there is one: Esc on that ask leaves none.
func ChooseTimeline(ctx context.Context, s FormSession, picker TimelinePicker, in FieldValues) (FieldValues, bool, error) {
	var by SelectedBy
	err := WithStorePath(ctx, s.App, s.Inv.Cfg.DBPath, func(st *store.Store) error {
		sel, err := SelectTimelines(ctx, s.App.Logger(), st, TimelineInputOf(in), s.App.LookupEnv)
		if err != nil || sel.By != "" {
			by = sel.By
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

// TimelinePicker asks for a timeline through Def's timeline field: one of timelines, opening on
// current, else the first, or, when create is set, ✨ New timeline, which create asks for.
type TimelinePicker struct {
	Def       Definition
	timelines []timeline.Timeline
	current   string
	create    TimelineAsk
}

// ask returns the name of the timeline picked, and whether there is one: Esc leaves none.
func (p TimelinePicker) ask(ctx context.Context, s FormSession) (string, bool, error) {
	var options []huh.Option[string]
	if p.create != nil {
		// An empty value is no timeline, which the field's check never rejects.
		options = append(options, huh.NewOption("✨ New timeline", ""))
	}
	for _, t := range p.timelines {
		options = append(options, huh.NewOption(strings.TrimSpace(t.Name+"  "+PickerLabel(t.Title)), t.Name))
	}
	pick := OnlyField(p.Def, "timeline")
	opening := FieldValues{"timeline": {cmp.Or(p.current, p.timelines[0].Name)}}
	fields := pick.FormFields(BaseCheckEnv(s), opening, FormChoices{Options: map[string][]huh.Option[string]{"timeline": options}})

	submitted, err := s.Ask(ctx, s.Frame, FormGroup{Fields: fields})
	switch {
	case err != nil:
		return "", false, AskError(err)
	case !submitted:
		return "", false, nil
	}
	name := pick.FormValues(fields).Value("timeline")
	if name == nil {
		return p.create(ctx, s)
	}

	return *name, true, nil
}

// switchTimeline asks through picker which timeline the entry typed goes to, and returns what the
// entry reopens with: typed, with the timeline and id in gives, which no entry asks, on the timeline
// picked or created, if any.
func switchTimeline(ctx context.Context, s FormSession, picker TimelinePicker, in, typed FieldValues) (FieldValues, error) {
	reopened := maps.Clone(typed)
	for _, name := range []string{"timeline", "id"} {
		if in.Given(name) {
			reopened[name] = in.Values(name)
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
func storedTimelines(ctx context.Context, s FormSession) ([]timeline.Timeline, error) {
	var timelines []timeline.Timeline
	err := WithStorePath(ctx, s.App, s.Inv.Cfg.DBPath, func(st *store.Store) error {
		var err error
		timelines, err = st.Timelines(ctx)
		return err
	})

	return timelines, err
}

// selectedTimeline returns the timeline in selects, which an entry prefilled from in adds to.
func selectedTimeline(ctx context.Context, s FormSession, in FieldValues) (timeline.Timeline, error) {
	var t timeline.Timeline
	err := WithStorePath(ctx, s.App, s.Inv.Cfg.DBPath, func(st *store.Store) error {
		var err error
		t, err = SelectOne(ctx, s.App.Logger(), st, TimelineInputOf(in), s.App.LookupEnv)
		return err
	})

	return t, err
}

// entry asks for one entry, prefilled from in, and adds it to in's timeline once submitted. It returns
// the values last submitted, or in when none was, and the summary's line for the record it stored;
// errEntryCancelled when Esc leaves it, and what was typed with ErrSwitchTimeline when ctrl+t does.
func (f AddForm[R]) entry(ctx context.Context, s FormSession, in FieldValues) (FieldValues, []SummaryLine, error) {
	choices, err := f.entryChoices(ctx, s, in, BaseCheckEnv(s).Zone)
	if err != nil {
		return in, nil, err
	}

	// The picker chose the timeline, and an id only a flag gives applies to the first entry.
	def := WithoutField(WithoutField(f.Def, "timeline"), "id")
	if i := def.FieldIndex("source"); i != -1 {
		// SourceChoices suggests every stored source.
		def.Fields[i].Preview = storedPreview("source", CheckSource, choices.Suggestions["source"])
	}
	if i := def.FieldIndex("tag"); i != -1 {
		// eventChoices suggests every stored tag.
		def.Fields[i].Preview = storedPreview("tag", CheckTag, choices.Suggestions["tag"])
	}

	var added []SummaryLine
	typed, submitted, err := s.AskUntil(ctx, def, choices, in, func(typed FieldValues) error {
		if in.Given("timeline") {
			typed["timeline"] = in.Values("timeline")
		}
		if in.Given("id") {
			typed["id"] = in.Values("id")
		}
		result, err := f.Submit(ctx, s, typed)
		if err != nil {
			return err
		}
		if line, stored := result.Summary(); stored {
			added = append(added, line)
		}
		return nil
	})
	if err == nil && !submitted {
		err = errEntryCancelled
	}

	return typed, added, err
}

// AskUntil asks def's fields, prefilled from in and picking from choices, and passes what was typed to
// submit. It asks again with what was typed while submit returns ErrAskAgain, or a correctable error,
// which it prints. It returns what was last typed, whether submit took it, and submit's error; in when
// Esc ends the form, or what was typed and ErrSwitchTimeline when ctrl+t does.
func (s FormSession) AskUntil(
	ctx context.Context, def Definition, choices FormChoices, in FieldValues, submit func(typed FieldValues) error,
) (FieldValues, bool, error) {
	choices.Thumbnails = s.thumbnails
	choices.Completions = s.completions
	choices.Paths = s.paths
	choices.placeholders = s.placeholders
	for {
		fields := def.FormFields(BaseCheckEnv(s), in, choices)
		groups := def.FormGroups(fields, in)
		submitted, err := s.Ask(ctx, s.Frame, groups...)
		typed := def.FormValues(def.shownFields(groups))
		switch {
		case errors.Is(err, ErrSwitchTimeline):
			return typed, false, err
		case err != nil:
			return in, false, AskError(err)
		case !submitted:
			return in, false, nil
		}

		err = submit(typed)
		rejected, _ := errors.AsType[*Error](err)
		switch {
		case errors.Is(err, ErrAskAgain):
		case Correctable(err):
			// A rejected entry is reported to the person at the form, never as a JSON envelope.
			err = WriteError(s.App.Err, rejected, ErrorStyle{color: s.Inv.ErrColor})
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
func (f AddForm[R]) entryChoices(ctx context.Context, s FormSession, in FieldValues, z Zone) (FormChoices, error) {
	var choices FormChoices
	err := WithStorePath(ctx, s.App, s.Inv.Cfg.DBPath, func(st *store.Store) error {
		t, err := SelectOne(ctx, s.App.Logger(), st, TimelineInputOf(in), s.App.LookupEnv)
		if err != nil {
			return err
		}
		choices, err = f.Choices(ctx, st, t, z)
		return err
	})

	return choices, err
}

// Correctable reports whether err rejects a submission the user can fix and submit again: usage, a
// taken timeline name, or a record not found other than the timeline the form writes to.
func Correctable(err error) bool {
	rejected, ok := errors.AsType[*Error](err)
	return ok && (rejected.Kind.ExitCode() == 2 || rejected.Kind.ExitCode() == 3 || rejected.Kind == KindTimelineExists) &&
		rejected.Kind != KindTimelineNotFound && rejected.Kind != KindTimelineRequired
}

// Submit runs values as the command line would, and prints the result and the flags-only command it
// equals, against the same database. It returns the result.
func (w FormWrite[R]) Submit(ctx context.Context, s FormSession, values FieldValues) (R, error) {
	result, err := w.Run(ctx, s.App, s.Inv.Cfg, values)
	if err != nil {
		return result, err
	}
	result.LogChanges(ctx, s.App.Logger())
	err = result.Render(s.Inv.Out)
	if err != nil {
		return result, err
	}

	return result, s.Echo(w.Path, w.Def, values)
}

// Echo prints the flags-only command that gives values to the command at path, with def's
// fields, against the same database.
func (s FormSession) Echo(path string, def Definition, values FieldValues) error {
	args := CommandArgs(path, def, values)
	label := newNoteStyles(s.Inv.ErrColor).faint.Render("Equivalent command:")

	return s.Inv.Out.Notify("%s %s", label, ShellLine(slices.Concat(args[:1], s.Inv.EchoArgs, args[1:])))
}

// BaseCheckEnv is what a form's checks read time input against before a zone is typed into it:
// now, in --tz, else the config file's default_timezone, or UTC when that is invalid,
// which the add reports where it reads it.
func BaseCheckEnv(s FormSession) CheckEnv {
	now := s.App.Now()
	z, err := TimeZone(s.Inv.TZ, s.Inv.Cfg.DefaultTimezone, now.Location())
	if err != nil {
		z = Zone{Loc: time.UTC, Label: "UTC"}
	}

	return CheckEnv{Now: now, Zone: z}
}

// AskError returns err as the command reports it: Ctrl-C is interrupted.
func AskError(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: err}
	}

	return err
}

// CarryForward returns values without d's PerEntry fields, as the next entry opens with.
func CarryForward(values FieldValues, d Definition) FieldValues {
	next := maps.Clone(values)
	for _, f := range d.Fields {
		if f.PerEntry {
			delete(next, f.Name)
		}
	}

	return next
}

// WithoutField returns d without the field called name.
func WithoutField(d Definition, name string) Definition {
	return Definition{Fields: slices.DeleteFunc(slices.Clone(d.Fields), func(f Field) bool { return f.Name == name })}
}

// OnlyField returns d with only the field called name.
func OnlyField(d Definition, name string) Definition {
	return Definition{Fields: slices.DeleteFunc(slices.Clone(d.Fields), func(f Field) bool { return f.Name != name })}
}

// TerminalForms returns the AskFunc that runs forms framed under title on app's terminal, drawing in
// color when color is true, in the profile an attach field needs for the images the terminal shows.
func TerminalForms(app App, title string) func(color bool, images graphics.Protocol) AskFunc {
	return func(color bool, images graphics.Protocol) AskFunc {
		return func(ctx context.Context, f FormFrame, groups ...FormGroup) (bool, error) {
			fields := FieldsOf(groups)
			attach := slices.ContainsFunc(fields, func(f huh.Field) bool { return f.GetKey() == "attach" })
			profile := FormProfile(color, images, attach)
			var keys FormKeys
			form := NewForm(f.Edges(title), groups...)
			// Not tea.Interrupt for cancel: bubbletea skips an interrupted program's last frame, the
			// empty one that clears the form.
			form.SubmitCmd, form.CancelCmd = tea.Quit, tea.Quit
			// The caller's context already ends on a signal, and the profile is pinned so bubbletea
			// never reads the process environment to pick one.
			_, err := tea.NewProgram(form, tea.WithContext(ctx), tea.WithInput(app.In), tea.WithOutput(app.Out),
				tea.WithFilter(keys.Filter(f, fields)), tea.WithColorProfile(profile), tea.WithoutSignalHandler()).Run()

			return keys.Ended(form.State, err)
		}
	}
}

// FormKeys records which key the program filter it returns turned into a form's own abort.
type FormKeys struct{ cancelled, switched bool }

// Filter returns the program filter of a form framed as f, of fields: EscCancels, and on an entry, one
// that turns ctrl+t into the form's own abort too, so no field sees it.
func (k *FormKeys) Filter(f FormFrame, fields []huh.Field) func(tea.Model, tea.Msg) tea.Msg {
	esc := EscCancels(&k.cancelled, fields)
	if !f.Entry {
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

// Ended returns what an ask returns for its form, which ended in state with err.
func (k *FormKeys) Ended(state huh.FormState, err error) (bool, error) {
	switch {
	case k.switched:
		return false, ErrSwitchTimeline
	case k.cancelled:
		return false, nil
	case state == huh.StateAborted:
		return false, huh.ErrUserAborted
	}

	return err == nil, err
}

// EscCancels returns a program filter that turns Esc into the form's own abort, so the form clears
// itself from the screen, and records in cancelled that Esc, not Ctrl-C, ended it. While one of
// fields filters its options, Esc is that field's.
func EscCancels(cancelled *bool, fields []huh.Field) func(tea.Model, tea.Msg) tea.Msg {
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

// SourceChoices suggests t's sources for the source field, the one on its latest event first, then
// every other source there is.
func SourceChoices(ctx context.Context, s *store.Store, t timeline.Timeline) (FormChoices, error) {
	timelineSources, err := s.TimelineSources(ctx, t.ID)
	if err != nil {
		return FormChoices{}, err
	}
	everySource, err := s.Sources(ctx)
	if err != nil {
		return FormChoices{}, err
	}
	names := AppendUnique(sourceNames(timelineSources), sourceNames(everySource)...)

	return FormChoices{Suggestions: map[string][]string{"source": names}}, nil
}

// storedPreview returns the preview of a field naming a noun: the hint of the check a typed value
// fails, else whether it is one of existing or a new noun.
func storedPreview(noun string, check func(string) error, existing []string) func(string, CheckEnv) string {
	return func(value string, _ CheckEnv) string {
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

// PickerLabel returns stored text as one line a picker can draw, so it cannot move the cursor or
// send the terminal a sequence: no escape sequences or control characters, white space collapsed,
// at most 60 cells wide.
func PickerLabel(text string) string {
	printable := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, ansi.Strip(text))

	return ansi.Truncate(strings.Join(strings.Fields(printable), " "), 60, "…")
}
