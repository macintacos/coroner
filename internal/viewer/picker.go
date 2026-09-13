package viewer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/macintacos/coroner/internal/timeline"
)

const maxTitleWidth = 32

// TimelineRow is one timeline the picker lists.
type TimelineRow struct {
	Timeline timeline.Timeline
	Events   int
	// Last is the timeline's last event in timeline.CompareEvents order, nil when it has none.
	Last *timeline.Event
	// Current marks the timeline 'coroner timeline use' stored.
	Current bool
}

// PickOptions is what the picker lists.
type PickOptions struct {
	Rows []TimelineRow
	// Notice is shown above the rows; "" shows none.
	Notice string
	// Location is the zone last event times show in.
	Location *time.Location
}

// Pick shows the picker on term until enter, which returns the timeline under the cursor, q or
// esc, which return ok false, Ctrl-C, which returns ErrInterrupted, or the end of ctx.
func Pick(ctx context.Context, term Terminal, opts PickOptions) (timeline.Timeline, bool, error) {
	profile := colorprofile.ANSI
	if term.NoColor {
		profile = colorprofile.ASCII
	}

	final, err := tea.NewProgram(NewPicker(opts), tea.WithContext(ctx), tea.WithInput(term.In), tea.WithOutput(term.Out),
		tea.WithColorProfile(profile), tea.WithoutSignalHandler()).Run()
	switch {
	case errors.Is(err, tea.ErrInterrupted):
		return timeline.Timeline{}, false, ErrInterrupted
	case err != nil:
		return timeline.Timeline{}, false, err
	}

	p, _ := final.(Picker)
	t, ok := p.Picked()
	return t, ok, nil
}

// Picker is the timeline picker's state; build it with NewPicker.
type Picker struct {
	list   list.Model
	notice string
	styles styles
	picked *timeline.Timeline
	width  int
}

// NewPicker returns the picker listing opts.Rows with the cursor on the current timeline, sized
// for an 80 by 24 terminal until the first window size arrives.
func NewPicker(opts PickOptions) Picker {
	s := newStyles()
	items := make([]list.Item, 0, len(opts.Rows))
	fields := make([][]string, 0, len(opts.Rows))
	for _, row := range opts.Rows {
		f := pickerFields(row, opts.Location)
		items = append(items, pickerItem{timeline: row.Timeline, fields: f})
		fields = append(fields, f)
	}

	l := list.New(items, pickerDelegate{widths: columnWidths(fields), styles: s}, 0, 0)
	l.Title = "Timelines"
	l.SetStatusBarItemName("timeline", "timelines")
	l.Styles = listStyles(s)
	l.FilterInput.SetStyles(filterStyles())
	l.Paginator.ActiveDot = l.Styles.ActivePaginationDot.String()
	l.Paginator.InactiveDot = l.Styles.InactivePaginationDot.String()
	l.KeyMap.Quit = key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit"))
	use := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "use"))
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{use} }
	l.AdditionalFullHelpKeys = l.AdditionalShortHelpKeys
	if i := slices.IndexFunc(opts.Rows, func(r TimelineRow) bool { return r.Current }); i >= 0 {
		l.Select(i)
	}

	return Picker{list: l, notice: opts.Notice, styles: s}.resize(80, 24)
}

// Picked returns the timeline enter picked, and false when none was.
func (p Picker) Picked() (timeline.Timeline, bool) {
	if p.picked == nil {
		return timeline.Timeline{}, false
	}

	return *p.picked, true
}

// Init starts nothing: the rows arrive through PickOptions.
func (p Picker) Init() tea.Cmd {
	return nil
}

// Update applies msg.
func (p Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return p.resize(msg.Width, msg.Height), nil
	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			return p, tea.Interrupt
		case msg.String() == "enter" && p.list.FilterState() != list.Filtering:
			if item, ok := p.list.SelectedItem().(pickerItem); ok {
				p.picked = &item.timeline
				return p, tea.Quit
			}
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

// View draws the notice, then the list.
func (p Picker) View() tea.View {
	v := tea.NewView(p.noticeView() + p.list.View())
	v.AltScreen = true
	return v
}

func (p Picker) resize(width, height int) Picker {
	p.width = width
	p.list.SetSize(width, max(height-strings.Count(p.noticeView(), "\n"), 1))
	return p
}

// noticeView is the notice, wrapped to the window, and a line break; "" when there is none.
func (p Picker) noticeView() string {
	if p.notice == "" {
		return ""
	}

	return p.styles.gap.PaddingLeft(len(indent)).Width(p.width).Render(oneLine(p.notice)) + "\n"
}

// pickerFields returns row's marker, "*" on the current timeline, then its name, event count,
// last event time in loc, title and refs: the fields a narrow window cuts come last.
func pickerFields(row TimelineRow, loc *time.Location) []string {
	t := row.Timeline
	marker, title, refs, last := " ", "-", "-", "-"
	if row.Current {
		marker = "*"
	}
	if t.Title != "" {
		title = oneLine(t.Title)
	}
	if len(t.Refs) > 0 {
		refs = oneLine(strings.Join(t.Refs, ", "))
	}
	if row.Last != nil {
		last = "last " + wall(*row.Last, loc).Format(time.DateOnly) + " " + strings.TrimSpace(eventTime(*row.Last, loc))
	}

	return []string{marker, t.Name, fmt.Sprintf("%d %s", row.Events, plural(row.Events, "event")), last, title, refs}
}

// columnWidths returns the widest of each field across rows, capping the title.
func columnWidths(rows [][]string) []int {
	caps := []int{0, 0, 0, 0, maxTitleWidth, 0}
	widths := make([]int, len(caps))
	for _, fields := range rows {
		for i, field := range fields {
			width := lipgloss.Width(field)
			if caps[i] > 0 {
				width = min(width, caps[i])
			}
			widths[i] = max(widths[i], width)
		}
	}

	return widths
}

type pickerItem struct {
	timeline timeline.Timeline
	fields   []string
}

// FilterValue is what typing after / matches: the name and title.
func (i pickerItem) FilterValue() string {
	return i.timeline.Name + " " + i.timeline.Title
}

// pickerDelegate draws each timeline on one line, its fields in aligned columns.
type pickerDelegate struct {
	widths []int
	styles styles
}

func (d pickerDelegate) Height() int { return 1 }

func (d pickerDelegate) Spacing() int { return 0 }

func (d pickerDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d pickerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(pickerItem)
	if !ok {
		return
	}

	fields := make([]string, len(it.fields))
	for i, field := range it.fields {
		fields[i] = pad(ansi.Truncate(field, d.widths[i], "…"), d.widths[i])
	}

	s := d.styles
	if index == m.Index() {
		_, _ = io.WriteString(w, s.selected.Render(ansi.Truncate("› "+strings.Join(fields, "  "), m.Width(), "…")))
		return
	}

	styled := []string{
		fields[0], s.name.Render(fields[1]), s.faint.Render(fields[2]), s.faint.Render(fields[3]), fields[4], s.source.Render(fields[5]),
	}
	_, _ = io.WriteString(w, ansi.Truncate(indent+strings.Join(styled, "  "), m.Width(), "…"))
}

// listStyles are the list's styles in the 16 ANSI colors, with bold and faint in place of shades.
func listStyles(s styles) list.Styles {
	plain := lipgloss.NewStyle()
	return list.Styles{
		TitleBar:                    plain.PaddingLeft(len(indent)),
		Title:                       s.name,
		Spinner:                     s.faint,
		DefaultFilterCharacterMatch: plain.Underline(true),
		StatusBar:                   s.faint.Padding(0, 0, 1, len(indent)),
		StatusEmpty:                 s.faint,
		StatusBarActiveFilter:       plain,
		StatusBarFilterCount:        s.faint,
		NoItems:                     s.faint,
		PaginationStyle:             plain.PaddingLeft(len(indent)),
		HelpStyle:                   plain.Padding(1, 0, 0, len(indent)),
		ActivePaginationDot:         plain.Bold(true).SetString("•"),
		InactivePaginationDot:       s.faint.SetString("•"),
		ArabicPagination:            s.faint,
		DividerDot:                  s.faint.SetString(" • "),
	}
}

// filterStyles styles the filter prompt in ANSI colors, with a cursor that does not blink.
func filterStyles() textinput.Styles {
	f := textinput.DefaultStyles(true)
	f.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	f.Blurred.Prompt = f.Focused.Prompt
	f.Focused.Placeholder = lipgloss.NewStyle().Faint(true)
	f.Blurred.Placeholder = f.Focused.Placeholder
	f.Focused.Suggestion = f.Focused.Placeholder
	f.Blurred.Suggestion = f.Focused.Placeholder
	f.Blurred.Text = lipgloss.NewStyle()
	f.Cursor.Color = lipgloss.White
	f.Cursor.Blink = false

	return f
}
