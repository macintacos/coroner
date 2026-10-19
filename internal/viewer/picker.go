package viewer

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timetext"
)

// TimelineRow is one timeline the picker lists.
type TimelineRow struct {
	Timeline   timeline.Timeline
	EventCount int
	// Last is the timeline's last event in timeline.CompareEvents order, nil when it has none.
	Last *timeline.Event
	// Current marks the timeline 'coroner timeline use' stored.
	Current bool
}

// PickOptions is what the picker lists and says, and the zone its times show in.
type PickOptions struct {
	Rows []TimelineRow
	// Notice is shown above the rows; "" shows none.
	Notice string
	// Location is the zone last event times show in.
	Location *time.Location
}

// Pick shows the picker on term until enter, which returns the timeline under the cursor, q, or
// esc when no filter is applied, which return ok false, Ctrl-C, which returns ErrInterrupted, or
// the end of ctx.
func Pick(ctx context.Context, term Terminal, opts PickOptions) (timeline.Timeline, bool, error) {
	final, err := program(ctx, term, NewPicker(opts))
	if err != nil {
		return timeline.Timeline{}, false, err
	}

	p, _ := final.(Picker)
	t, ok := p.Picked()
	return t, ok, nil
}

// Picker is the timeline picker's state; build it with NewPicker.
type Picker struct {
	list           list.Model
	notice         string
	styles         styles
	picked         *timeline.Timeline
	interrupt, use key.Binding
}

// NewPicker returns the picker listing opts.Rows with the cursor on the current timeline, sized
// for an 80 by 24 terminal until the first window size arrives.
func NewPicker(opts PickOptions) Picker {
	s := newStyles()
	items := make([]list.Item, 0, len(opts.Rows))
	columns := make([]pickerColumns, 0, len(opts.Rows))
	for _, row := range opts.Rows {
		c := pickerFields(row, opts.Location)
		items = append(items, pickerItem{timeline: row.Timeline, columns: c})
		columns = append(columns, c)
	}

	l := list.New(items, pickerDelegate{widths: measureColumns(columns), styles: s}, 0, 0)
	l.Title = "Timelines"
	l.SetStatusBarItemName("timeline", "timelines")
	l.Styles = listStyles(s)
	l.Help.Styles = helpStyles(s)
	l.FilterInput.SetStyles(filterStyles())
	// list.New copied the dots from its default styles.
	l.Paginator.ActiveDot = l.Styles.ActivePaginationDot.String()
	l.Paginator.InactiveDot = l.Styles.InactivePaginationDot.String()
	// bubbles v2.2.1's default Quit is bound to v.
	l.KeyMap.Quit = key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit"))
	use := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "use"))
	l.AdditionalShortHelpKeys = func() []key.Binding { return []key.Binding{use} }
	l.AdditionalFullHelpKeys = l.AdditionalShortHelpKeys
	if i := slices.IndexFunc(opts.Rows, func(r TimelineRow) bool { return r.Current }); i >= 0 {
		l.Select(i)
	}

	return Picker{list: l, notice: opts.Notice, styles: s, interrupt: newKeyMap().interrupt, use: use}.resize(80, 24)
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
		// Before the list, whose ForceQuit quits rather than interrupts.
		case key.Matches(msg, p.interrupt):
			return p, tea.Interrupt
		case key.Matches(msg, p.use) && p.list.FilterState() != list.Filtering:
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
	content := p.list.View()
	if notice := p.noticeView(); notice != "" {
		content = notice + "\n" + content
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (p Picker) resize(width, height int) Picker {
	p.list.SetWidth(width)
	if notice := p.noticeView(); notice != "" {
		height -= lipgloss.Height(notice)
	}
	p.list.SetHeight(max(height, 1))
	return p
}

// noticeView is the notice, wrapped to the window; "" when there is none.
func (p Picker) noticeView() string {
	if p.notice == "" {
		return ""
	}

	return p.styles.notice.PaddingLeft(len(indent)).Width(p.list.Width()).Render(oneLine(p.notice))
}

// pickerColumns are the fields of a timeline's row.
type pickerColumns struct {
	marker, name, events, last, title, refs string
}

// pickerFields returns row's fields: its marker, "*" on the current timeline, name, event count,
// last event time in loc, title and refs.
func pickerFields(row TimelineRow, loc *time.Location) pickerColumns {
	t := row.Timeline
	c := pickerColumns{
		marker: " ", name: t.Name, events: fmt.Sprintf("%d %s", row.EventCount, plural(row.EventCount, "event")),
		last: "-", title: "-", refs: "-",
	}
	if row.Current {
		c.marker = "*"
	}
	if t.Title != "" {
		c.title = oneLine(t.Title)
	}
	if len(t.Refs) > 0 {
		c.refs = oneLine(strings.Join(t.Refs, ", "))
	}
	if row.Last != nil {
		c.last = "last " + timetext.Read(*row.Last, loc).Day + " " + strings.TrimSpace(eventTime(*row.Last, loc))
	}

	return c
}

// pickerWidths are the picker's column widths.
type pickerWidths struct {
	name, events, last int
}

// measureColumns returns the widest of each column, capping the name: a longer name is never cut,
// and pushes only its own title.
func measureColumns(rows []pickerColumns) pickerWidths {
	var w pickerWidths
	for _, c := range rows {
		w.name = max(w.name, min(lipgloss.Width(c.name), maxNameWidth))
		w.events = max(w.events, lipgloss.Width(c.events))
		w.last = max(w.last, lipgloss.Width(c.last))
	}

	return w
}

type pickerItem struct {
	timeline timeline.Timeline
	columns  pickerColumns
}

// FilterValue is what typing after / matches: the name and title.
func (i pickerItem) FilterValue() string {
	return i.timeline.Name + " " + i.timeline.Title
}

// pickerDelegate draws each timeline on two lines, in aligned columns: its marker, name and title,
// then, under the name, its event count, last event and refs.
type pickerDelegate struct {
	widths pickerWidths
	styles styles
}

func (d pickerDelegate) Height() int { return 2 }

func (d pickerDelegate) Spacing() int { return 1 }

func (d pickerDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d pickerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(pickerItem)
	if !ok {
		return
	}

	c, s := it.columns, d.styles
	name, title := pad(c.name, d.widths.name), ansi.Truncate(c.title, maxTitleWidth, "…")
	details := pad(c.events, d.widths.events) + "  " + pad(c.last, d.widths.last) + "  " + c.refs
	fit := func(line string) string { return ansi.Truncate(line, m.Width(), "…") }
	if index == m.Index() {
		top, bottom := fit("› "+c.marker+"  "+name+"  "+title), fit("›    "+details)
		_, _ = io.WriteString(w, s.selected.Render(top)+"\n"+s.selected.Render(bottom))
		return
	}

	top, bottom := fit(indent+c.marker+"  "+s.name.Render(name)+"  "+title), fit(indent+"   "+s.faint.Render(details))
	_, _ = io.WriteString(w, top+"\n"+bottom)
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

// helpStyles are the help line's styles in the 16 ANSI colors.
func helpStyles(s styles) help.Styles {
	plain := lipgloss.NewStyle()
	return help.Styles{
		Ellipsis:       s.faint,
		ShortKey:       plain,
		ShortDesc:      s.faint,
		ShortSeparator: s.faint,
		FullKey:        plain,
		FullDesc:       s.faint,
		FullSeparator:  s.faint,
	}
}

// filterStyles styles the filter prompt in ANSI colors.
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

	return f
}
