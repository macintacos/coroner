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
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

// reportCellSize is the XTWINOPS operation asking the terminal for its cell size in pixels.
const reportCellSize = 16

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

// loadedMsg is the result of the load numbered seq, read under match.
type loadedMsg struct {
	snapshot Snapshot
	match    Match
	err      error
	seq      int
}

// imageMsg is the image attachment attachmentID holds, or why it cannot show.
type imageMsg struct {
	attachmentID string
	img          graphics.Image
	err          error
}

// shownImage is an image attachment once read: its size in pixels, unknown when it could not be
// read, and, once sent to the terminal, the id naming it there and the grid it is placed in.
type shownImage struct {
	img  graphics.Image
	id   uint32
	grid graphics.Grid
}

// Model is the viewer's state; build it with New.
type Model struct {
	opts     Options
	snapshot Snapshot
	// match is what snapshot was read under. pending is what the newest load reads under, and
	// what the next filter builds on.
	match, pending Match
	// seq numbers the newest load; a result from an older one is dropped.
	seq int
	// annotations holds the snapshot's annotations by event id.
	annotations map[string][]timeline.Annotation
	// attachments holds the snapshot's attachments by the id of their event or annotation.
	attachments map[string][]timeline.Attachment
	// expanded holds the ids of the events whose annotations and attachments show.
	expanded map[string]bool
	graphics graphics.Protocol
	// images holds the image attachments read or being read, by attachment id, and sent the id
	// last sent to the terminal.
	images map[string]shownImage
	sent   uint32
	// answered is whether the terminal has answered the attributes query Init sends, so it has
	// read the first frame, which switches to the alternate screen: an image sent to the main
	// screen never shows on the alternate one.
	answered bool
	// cell is the terminal's cell size in pixels, zero until it reports one.
	cell graphics.Pixels
	// lines are the event list's lines and blocks[i] where event i's sit in them, rebuilt only
	// when the snapshot, width or expanded events change.
	lines    []string
	blocks   []block
	widths   widths
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
		images:   map[string]shownImage{},
		input:    textinput.New(),
		keys:     newKeyMap(),
		help:     help.New(),
		body:     viewport.New(),
		width:    80,
		height:   24,
		styles:   newStyles(),
	}

	return m.show(opts.Snapshot, opts.Match)
}

// Init asks a terminal that shows images for its cell size and then its attributes, which every
// terminal answers; the first snapshot arrives through Options.
func (m Model) Init() tea.Cmd {
	if m.graphics != graphics.Kitty {
		return nil
	}

	return tea.Raw(ansi.WindowOp(reportCellSize) + ansi.RequestPrimaryDeviceAttributes)
}

// Update applies msg.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.placeImages()
	case uv.CellSizeEvent:
		m.cell = graphics.Pixels{Width: msg.Width, Height: msg.Height}
		return m.placeImages()
	case uv.PrimaryDeviceAttributesEvent:
		m.answered = true
		return m.sendImages()
	case loadedMsg:
		return m.loaded(msg), nil
	case imageMsg:
		return m.imageRead(msg)
	case tea.KeyPressMsg:
		return m.keyPressed(msg)
	}

	if m.prompt != noPrompt {
		return m.edit(msg)
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
	case key.Matches(msg, m.keys.help, m.keys.cancel):
		m.showHelp = false
	}

	return m, nil
}

func (m Model) listKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.refresh):
		return m.load(m.pending)
	case key.Matches(msg, m.keys.source):
		return m.openPrompt(sourcePrompt, strings.Join(m.pending.Sources, ", "))
	case key.Matches(msg, m.keys.tag):
		return m.openPrompt(tagPrompt, strings.Join(m.pending.Tags, ", "))
	case key.Matches(msg, m.keys.contains):
		return m.openPrompt(containsPrompt, m.pending.Contains)
	case key.Matches(msg, m.keys.jump):
		return m.openPrompt(jumpPrompt, "")
	case key.Matches(msg, m.keys.help):
		m.showHelp = true
		return m, nil
	case key.Matches(msg, m.keys.annotations):
		var read tea.Cmd
		if len(m.snapshot.Events) > 0 {
			id := m.snapshot.Events[m.cursor].ID
			m.expanded[id] = !m.expanded[id]
			if m.expanded[id] {
				read = m.readImages(id)
			}
		}
		return m.render(), read
	}

	return m.moveTo(m.cursor + m.movement(msg)), nil
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
	focus := m.input.Focus()

	return m.scroll(), focus
}

func (m Model) promptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.cancel):
		m.prompt = noPrompt
		m.input.Blur()
		return m, nil
	case key.Matches(msg, m.keys.submit):
		p := m.prompt
		m.prompt = noPrompt
		m.input.Blur()
		return m.submit(p, m.input.Value())
	}

	return m.edit(msg)
}

// edit hands msg, such as a key, a paste or a cursor blink, to the open prompt.
func (m Model) edit(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) submit(p prompt, value string) (tea.Model, tea.Cmd) {
	match := m.pending
	switch p {
	case sourcePrompt:
		match.Sources = splitNames(value)
	case tagPrompt:
		match.Tags = splitNames(value)
	case containsPrompt:
		match.Contains = value
		if strings.TrimSpace(value) == "" {
			match.Contains = ""
		}
	case jumpPrompt:
		return m.jump(strings.TrimSpace(value)), nil
	case noPrompt:
	}

	return m.load(match)
}

// splitNames returns the comma-separated names in value, trimmed and each once, leaving out
// empty ones.
func splitNames(value string) []string {
	var names []string
	for name := range strings.SplitSeq(value, ",") {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return names
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
		return m.scroll()
	}

	i := slices.IndexFunc(m.snapshot.Events, func(e timeline.Event) bool { return !e.OccurredAt.Before(r.Instant) })
	if i < 0 {
		i = len(m.snapshot.Events) - 1
	}

	return m.moveTo(i)
}

func failure(err error) string {
	text := err.Error()
	rejected, ok := errors.AsType[*timeparse.ParseError](err)
	if ok && rejected.Hint != "" {
		text += ". " + rejected.Hint
	}

	return text
}

// load starts reading the timeline again under match. A load still running is superseded.
func (m Model) load(match Match) (Model, tea.Cmd) {
	m.pending = match
	m.seq++
	load, seq := m.opts.Load, m.seq

	return m, func() tea.Msg {
		snapshot, err := load(match)
		return loadedMsg{snapshot: snapshot, match: match, err: err, seq: seq}
	}
}

func (m Model) loaded(msg loadedMsg) Model {
	switch {
	case msg.seq != m.seq:
		return m
	case msg.err != nil:
		m.problem = failure(msg.err)
		m.pending = m.match
		return m.scroll()
	}

	m.problem = ""
	return m.show(msg.snapshot, msg.match)
}

// show replaces what the viewer shows, keeping the cursor on its event when the snapshot still
// has it.
func (m Model) show(snapshot Snapshot, match Match) Model {
	cursorID := ""
	if len(m.snapshot.Events) > 0 {
		cursorID = m.snapshot.Events[m.cursor].ID
	}

	m.snapshot, m.match, m.pending = snapshot, match, match
	m.annotations = map[string][]timeline.Annotation{}
	for _, a := range snapshot.Annotations {
		if a.EventID != nil {
			m.annotations[*a.EventID] = append(m.annotations[*a.EventID], a)
		}
	}
	m.attachments = map[string][]timeline.Attachment{}
	for _, a := range snapshot.Attachments {
		owner := a.Owner.EventID
		if owner == nil {
			owner = a.Owner.AnnotationID
		}
		m.attachments[*owner] = append(m.attachments[*owner], a)
	}

	if i := slices.IndexFunc(snapshot.Events, func(e timeline.Event) bool { return e.ID == cursorID }); i >= 0 {
		m.cursor = i
	}

	return m.render()
}

// render keeps the cursor on an event and rebuilds the event list's lines.
func (m Model) render() Model {
	m.cursor = max(min(m.cursor, len(m.snapshot.Events)-1), 0)
	m.widths = measure(m.snapshot.Events, m.opts.Location)
	m.lines, m.blocks = m.bodyLines()

	return m.scroll()
}

// moveTo puts the cursor on event i, or the nearest event there is, redrawing only the two rows
// that change.
func (m Model) moveTo(i int) Model {
	i = max(min(i, len(m.snapshot.Events)-1), 0)
	if i == m.cursor {
		return m
	}

	// Cloned, because an earlier Model and the viewport still hold the old lines.
	m.lines = slices.Clone(m.lines)
	m.lines[m.blocks[m.cursor].row] = m.row(m.snapshot.Events[m.cursor], false)
	m.cursor = i
	m.lines[m.blocks[i].row] = m.row(m.snapshot.Events[i], true)

	return m.scroll()
}

// scroll sizes the event list to the window and scrolls it so the cursor event, the rules above
// it and its annotations show.
func (m Model) scroll() Model {
	m.body.SetWidth(m.width)
	m.body.SetHeight(m.bodyHeight())
	m.body.SetContentLines(m.lines)

	var cursor block
	if len(m.blocks) > 0 {
		cursor = m.blocks[m.cursor]
	}
	offset := m.body.YOffset()
	if cursor.bottom >= offset+m.body.Height() {
		offset = cursor.bottom - m.body.Height() + 1
	}
	m.body.SetYOffset(min(offset, cursor.top))

	return m
}

// bodyHeight is how many lines the event list gets in the window.
func (m Model) bodyHeight() int {
	return max(m.height-lipgloss.Height(m.header())-lipgloss.Height(m.footer()), 1)
}

// readImages starts reading each image attachment on the event with eventID or its annotations
// that has not been read yet.
func (m Model) readImages(eventID string) tea.Cmd {
	owned := slices.Clone(m.attachments[eventID])
	for _, a := range m.annotations[eventID] {
		owned = append(owned, m.attachments[a.ID]...)
	}

	var reads []tea.Cmd
	for _, a := range owned {
		if _, seen := m.images[a.ID]; seen || !isImage(a) {
			continue
		}
		m.images[a.ID] = shownImage{}
		reads = append(reads, readImage(m.opts.Blob, a))
	}

	return tea.Batch(reads...)
}

func readImage(blob func(timeline.Attachment) ([]byte, error), a timeline.Attachment) tea.Cmd {
	return func() tea.Msg {
		content, err := blob(a)
		if err != nil {
			return imageMsg{attachmentID: a.ID, err: err}
		}
		img, err := graphics.Decode(content)

		return imageMsg{attachmentID: a.ID, img: img, err: err}
	}
}

// imageRead keeps the image msg read, or nothing of it when it could not be read, and sends it.
func (m Model) imageRead(msg imageMsg) (Model, tea.Cmd) {
	shown := shownImage{}
	if msg.err == nil {
		shown.img = msg.img
	}
	m.images[msg.attachmentID] = shown

	return m.sendImages()
}

// sendImages rebuilds the event list and, once a terminal that shows images has answered, sends
// it each image read and not sent yet.
func (m Model) sendImages() (Model, tea.Cmd) {
	var sends strings.Builder
	for _, a := range m.snapshot.Attachments {
		shown := m.images[a.ID]
		if m.graphics != graphics.Kitty || !m.answered || shown.img.PNG == nil || shown.id != 0 {
			continue
		}
		m.sent++
		shown.id, shown.grid = m.sent, m.gridFor(a, shown.img)
		m.images[a.ID] = shown
		sends.WriteString(graphics.Transmit(shown.id, shown.img.PNG) + graphics.Place(shown.id, shown.grid))
	}

	return m.render(), raw(sends.String())
}

// raw is the command writing seq to the terminal, or nil for an empty seq.
func raw(seq string) tea.Cmd {
	if seq == "" {
		return nil
	}

	return tea.Raw(seq)
}

// placeImages rebuilds the event list and places each image sent to the terminal again whose
// grid the window or cell size changed.
func (m Model) placeImages() (Model, tea.Cmd) {
	var places strings.Builder
	for _, a := range m.snapshot.Attachments {
		shown, ok := m.images[a.ID]
		if !ok || shown.id == 0 {
			continue
		}
		if grid := m.gridFor(a, shown.img); grid != shown.grid {
			shown.grid = grid
			m.images[a.ID] = shown
			places.WriteString(graphics.Place(shown.id, grid))
		}
	}

	return m.render(), raw(places.String())
}

// gridFor is the grid attachment a shows img in: as wide as the window leaves beside its lines'
// indent, and no taller than the event list less its caption.
func (m Model) gridFor(a timeline.Attachment, img graphics.Image) graphics.Grid {
	bounds := graphics.Grid{Cols: m.width - attachmentIndentWidth(a), Rows: m.bodyHeight() - 1}
	return graphics.Fit(img, bounds, m.cell)
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
