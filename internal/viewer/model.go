package viewer

import (
	"errors"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

type prompt int

const (
	noPrompt prompt = iota
	sourcePrompt
	tagPrompt
	containsPrompt
	jumpPrompt
)

func (p prompt) label() string {
	switch p {
	case sourcePrompt:
		return "Sources, any of: "
	case tagPrompt:
		return "Tags, all of: "
	case containsPrompt:
		return "Text contains: "
	case jumpPrompt:
		return "Jump to time: "
	case noPrompt:
	}

	return ""
}

// loadedMsg is a finished Load under filter, and the event the cursor was on when it began.
type loadedMsg struct {
	snapshot Snapshot
	filter   store.EventFilter
	err      error
	cursorID string
}

// Model is the viewer's state; build it with New.
type Model struct {
	opts     Options
	snapshot Snapshot
	filter   store.EventFilter
	// annotations holds the snapshot's annotations by event id.
	annotations map[string][]timeline.Annotation
	// expanded holds the ids of the events whose annotations show.
	expanded map[string]bool
	cursor   int
	prompt   prompt
	input    textinput.Model
	showHelp bool
	// problem is why the last load, filter or jump failed, shown under the events.
	problem       string
	keys          keyMap
	help          help.Model
	body          viewport.Model
	width, height int
	styles        styles
}

// New returns the viewer showing opts.Snapshot, sized for an 80 by 24 terminal until the first
// window size arrives.
func New(opts Options) Model {
	m := Model{
		opts:     opts,
		expanded: map[string]bool{},
		input:    textinput.New(),
		keys:     newKeyMap(),
		help:     help.New(),
		body:     viewport.New(),
		width:    80,
		height:   24,
		styles:   newStyles(),
	}

	return m.show(opts.Snapshot, opts.Filter, "")
}

// Init starts nothing: the first snapshot arrives through Options.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update applies msg.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.layout(), nil
	case loadedMsg:
		return m.loaded(msg), nil
	case tea.KeyPressMsg:
		return m.keyPressed(msg)
	}

	return m, nil
}

func (m Model) keyPressed(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.interrupt):
		return m, tea.Interrupt
	case m.prompt != noPrompt:
		return m.promptKey(msg)
	case m.showHelp:
		return m.helpKey(msg)
	}

	return m.listKey(msg)
}

func (m Model) helpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.help, m.keys.closeHelp):
		m.showHelp = false
	}

	return m, nil
}

func (m Model) listKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.refresh):
		return m, m.load(m.filter)
	case key.Matches(msg, m.keys.source):
		return m.openPrompt(sourcePrompt, strings.Join(m.filter.Sources, ", "))
	case key.Matches(msg, m.keys.tag):
		return m.openPrompt(tagPrompt, strings.Join(m.filter.Tags, ", "))
	case key.Matches(msg, m.keys.contains):
		return m.openPrompt(containsPrompt, m.filter.Contains)
	case key.Matches(msg, m.keys.jump):
		return m.openPrompt(jumpPrompt, "")
	case key.Matches(msg, m.keys.help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, m.keys.annotations):
		if len(m.snapshot.Events) > 0 {
			id := m.snapshot.Events[m.cursor].ID
			m.expanded[id] = !m.expanded[id]
		}
	}

	m.cursor += m.movement(msg)
	return m.layout(), nil
}

// movement returns how many events msg moves the cursor by.
func (m Model) movement(msg tea.KeyPressMsg) int {
	page, count := max(m.body.Height(), 1), len(m.snapshot.Events)
	switch {
	case key.Matches(msg, m.keys.up):
		return -1
	case key.Matches(msg, m.keys.down):
		return 1
	case key.Matches(msg, m.keys.pageUp):
		return -page
	case key.Matches(msg, m.keys.pageDown):
		return page
	case key.Matches(msg, m.keys.top):
		return -count
	case key.Matches(msg, m.keys.bottom):
		return count
	}

	return 0
}

func (m Model) openPrompt(p prompt, value string) (tea.Model, tea.Cmd) {
	m.prompt = p
	m.problem = ""
	m.input.Prompt = p.label()
	m.input.SetValue(value)
	m.input.CursorEnd()

	return m, m.input.Focus()
}

func (m Model) promptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.prompt = noPrompt
		m.input.Blur()
		return m, nil
	case "enter":
		p, value := m.prompt, strings.TrimSpace(m.input.Value())
		m.prompt = noPrompt
		m.input.Blur()
		return m.submit(p, value)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submit applies value, entered at prompt p.
func (m Model) submit(p prompt, value string) (tea.Model, tea.Cmd) {
	f := m.filter
	var err error
	switch p {
	case sourcePrompt:
		f.Sources, err = splitNames(value, timeline.ValidateSourceName)
	case tagPrompt:
		f.Tags, err = splitNames(value, timeline.ValidateTag)
	case containsPrompt:
		f.Contains = value
	case jumpPrompt:
		return m.jump(value), nil
	case noPrompt:
	}

	if err != nil {
		m.problem = failure(err)
		return m, nil
	}

	return m, m.load(f)
}

// splitNames returns the comma-separated names in value, each of which must pass validate.
func splitNames(value string, validate func(string) error) ([]string, error) {
	var names []string
	for name := range strings.SplitSeq(value, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		err := validate(name)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}

	return names, nil
}

// jump moves the cursor to the first event at or after the time value names, or the last event
// when every one is earlier.
func (m Model) jump(value string) Model {
	if value == "" || len(m.snapshot.Events) == 0 {
		return m
	}

	r, err := timeparse.Parse(value, m.opts.Now(), m.opts.Location)
	if err != nil {
		m.problem = failure(err)
		return m
	}

	m.cursor = slices.IndexFunc(m.snapshot.Events, func(e timeline.Event) bool { return !e.OccurredAt.Before(r.Instant) })
	if m.cursor < 0 {
		m.cursor = len(m.snapshot.Events) - 1
	}

	return m.layout()
}

func failure(err error) string {
	text := err.Error()
	rejected, ok := errors.AsType[*timeparse.ParseError](err)
	if ok && rejected.Hint != "" {
		text += ". " + rejected.Hint
	}

	return text
}

// load returns the command that reads the timeline again under f.
func (m Model) load(f store.EventFilter) tea.Cmd {
	load, cursorID := m.opts.Load, ""
	if len(m.snapshot.Events) > 0 {
		cursorID = m.snapshot.Events[m.cursor].ID
	}

	return func() tea.Msg {
		snapshot, err := load(f)
		return loadedMsg{snapshot: snapshot, filter: f, err: err, cursorID: cursorID}
	}
}

func (m Model) loaded(msg loadedMsg) Model {
	if msg.err != nil {
		m.problem = failure(msg.err)
		return m
	}

	m.problem = ""
	return m.show(msg.snapshot, msg.filter, msg.cursorID)
}

// show replaces what the viewer shows, keeping the cursor on the event with cursorID when the
// snapshot still has it.
func (m Model) show(snapshot Snapshot, filter store.EventFilter, cursorID string) Model {
	m.snapshot, m.filter = snapshot, filter
	m.annotations = map[string][]timeline.Annotation{}
	for _, a := range snapshot.Annotations {
		if a.EventID != nil {
			m.annotations[*a.EventID] = append(m.annotations[*a.EventID], a)
		}
	}

	if i := slices.IndexFunc(snapshot.Events, func(e timeline.Event) bool { return e.ID == cursorID }); i >= 0 {
		m.cursor = i
	}

	return m.layout()
}

// layout keeps the cursor on an event, sizes the event list to the window and scrolls it so the
// cursor event, the rules above it and its annotations show.
func (m Model) layout() Model {
	m.cursor = max(min(m.cursor, len(m.snapshot.Events)-1), 0)
	lines, top, bottom := m.bodyLines()

	m.body.SetWidth(m.width)
	m.body.SetHeight(max(m.height-chromeHeight, 1))
	m.body.SetContentLines(lines)
	offset := m.body.YOffset()
	if bottom >= offset+m.body.Height() {
		offset = bottom - m.body.Height() + 1
	}
	m.body.SetYOffset(min(offset, top))

	return m
}

// View draws the header, the event list or the key help, the message line and the short help.
func (m Model) View() tea.View {
	body := m.body.View()
	if m.showHelp {
		body = m.keyHelp()
	}

	v := tea.NewView(strings.Join([]string{m.header(), body, m.footer()}, "\n"))
	v.AltScreen = true
	return v
}
