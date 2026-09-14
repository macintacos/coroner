package viewer

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	// gapThreshold is the shortest silence between events that gets its own row.
	gapThreshold   = 5 * time.Minute
	maxSourceWidth = 16
	maxTagsWidth   = 24
	maxNameWidth   = 32
	maxTitleWidth  = 32
	indent         = "  "
	// expandedIndent is where an expanded event's attachments and annotations start.
	expandedIndent = indent + "    "
)

type styles struct {
	name, faint, time, source, tags, gap, day, evidence, comment, selected, failure, notice lipgloss.Style
}

// newStyles uses only the 16 ANSI colors, so the terminal's theme picks the shades.
func newStyles() styles {
	s := lipgloss.NewStyle()
	return styles{
		name:     s.Bold(true),
		faint:    s.Faint(true),
		time:     s.Bold(true),
		source:   s.Foreground(lipgloss.Cyan),
		tags:     s.Foreground(lipgloss.Magenta),
		gap:      s.Bold(true).Foreground(lipgloss.Yellow),
		day:      s.Bold(true),
		evidence: s.Foreground(lipgloss.Green),
		comment:  s.Faint(true).Italic(true),
		selected: s.Reverse(true),
		failure:  s.Foreground(lipgloss.Red),
		notice:   s.Bold(true).Foreground(lipgloss.Yellow),
	}
}

// header names the timeline and the zone times show in, then counts the events the filter keeps.
func (m Model) header() string {
	t := m.snapshot.Timeline
	left := m.styles.name.Render(t.Name)
	if t.Title != "" {
		left += "  " + oneLine(t.Title)
	}
	right := m.styles.faint.Render("times in " + m.opts.ZoneLabel)
	space := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)

	return m.fit(left+strings.Repeat(" ", space)+right) + "\n" + m.fit(m.styles.faint.Render(m.summary()))
}

// summary counts the events and says what the filter keeps.
func (m Model) summary() string {
	count := strconv.Itoa(len(m.snapshot.Events)) + " events"
	if len(m.snapshot.Events) == 1 {
		count = "1 event"
	}

	var kept []string
	if len(m.match.Sources) > 0 {
		kept = append(kept, "from "+strings.Join(m.match.Sources, " or "))
	}
	if len(m.match.Tags) > 0 {
		kept = append(kept, "tagged "+strings.Join(m.match.Tags, " and "))
	}
	if m.match.Contains != "" {
		kept = append(kept, fmt.Sprintf("containing %q", m.match.Contains))
	}

	return strings.TrimSpace(count + " " + strings.Join(kept, ", "))
}

// footer is the open prompt or the last failure, wrapped so its hint is never cut, then the short
// help.
func (m Model) footer() string {
	line := ""
	switch {
	case m.prompt != noPrompt:
		line = m.fit(m.input.View())
	case m.problem != "":
		line = m.styles.failure.Width(m.width).Render(m.problem)
	}

	h := m.help
	h.SetWidth(m.width)
	return line + "\n" + h.ShortHelpView(m.keys.ShortHelp())
}

// keyHelp lists every key, filling the space the event list takes.
func (m Model) keyHelp() string {
	h := m.help
	h.SetWidth(m.width)
	lines := strings.Split(m.styles.name.Render("Keys")+"\n\n"+h.FullHelpView(m.keys.FullHelp()), "\n")
	height := m.body.Height()
	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines[:height], "\n")
}

// block is where an event's lines sit in the event list: the rules above it, its row and its
// annotations.
type block struct {
	top, row, bottom int
}

// bodyLines returns the event list's lines, and each event's block in them.
func (m Model) bodyLines() ([]string, []block) {
	events := m.snapshot.Events
	if len(events) == 0 {
		return []string{indent + m.emptyMessage()}, nil
	}

	gaps := map[int]time.Duration{}
	for _, g := range timeline.Gaps(events, gapThreshold) {
		gaps[g.Before] = g.Duration
	}

	lines := make([]string, 0, len(events))
	blocks := make([]block, len(events))
	day := ""
	for i, e := range events {
		top := len(lines)
		if d, ok := gaps[i]; ok {
			lines = append(lines, m.rule(formatGap(d), m.styles.gap))
		}
		if date := wall(e, m.opts.Location).Format("Mon 2006-01-02"); date != day {
			lines = append(lines, m.rule(date, m.styles.day))
			day = date
		}
		row := len(lines)
		lines = append(lines, m.row(e, i == m.cursor))
		if m.expanded[e.ID] {
			lines = append(lines, m.attachmentLines(m.attachments[e.ID], expandedIndent)...)
			lines = append(lines, m.annotationLines(e.ID)...)
		}
		blocks[i] = block{top: top, row: row, bottom: len(lines) - 1}
	}

	return lines, blocks
}

func (m Model) emptyMessage() string {
	if len(m.match.Sources) > 0 || len(m.match.Tags) > 0 || m.match.Contains != "" {
		return "No events match. Press s, t or / to change the filter."
	}

	return "No events yet. Add one with: coroner event add <text>"
}

// rule is a full-width line carrying label, such as a gap or a date.
func (m Model) rule(label string, style lipgloss.Style) string {
	text := indent + "── " + label + " "
	return m.fit(style.Render(text + strings.Repeat("─", max(m.width-lipgloss.Width(text), 2))))
}

// widths are the event list's column widths.
type widths struct {
	seq, time, source, tags int
}

func measure(events []timeline.Event, loc *time.Location) widths {
	var w widths
	for _, e := range events {
		w.seq = max(w.seq, lipgloss.Width(seqText(e)))
		w.time = max(w.time, lipgloss.Width(eventTime(e, loc)))
		w.source = max(w.source, min(lipgloss.Width(sourceText(e)), maxSourceWidth))
		w.tags = max(w.tags, min(lipgloss.Width(tagsText(e)), maxTagsWidth))
	}

	return w
}

// row is e's line: seq, time, source, tags, text and how many annotations and attachments it has.
func (m Model) row(e timeline.Event, selected bool) string {
	w := m.widths
	fields := []string{
		pad(seqText(e), w.seq),
		pad(eventTime(e, m.opts.Location), w.time),
		pad(ansi.Truncate(sourceText(e), w.source, "…"), w.source),
		pad(ansi.Truncate(tagsText(e), w.tags, "…"), w.tags),
		oneLine(e.Text),
	}
	var counts []string
	if n := len(m.annotations[e.ID]); n > 0 {
		counts = append(counts, fmt.Sprintf("%d %s", n, plural(n, "annotation")))
	}
	if n := len(m.attachments[e.ID]); n > 0 {
		counts = append(counts, fmt.Sprintf("%d %s", n, plural(n, "attachment")))
	}
	count := ""
	if len(counts) > 0 {
		count = "  (" + strings.Join(counts, ", ") + ")"
	}

	if selected {
		return m.styles.selected.Render(m.fit("› " + strings.Join(fields, "  ") + count))
	}

	styled := []string{
		m.styles.faint.Render(fields[0]), m.styles.time.Render(fields[1]), m.styles.source.Render(fields[2]),
		m.styles.tags.Render(fields[3]), fields[4] + m.styles.faint.Render(count),
	}
	return m.fit(indent + strings.Join(styled, "  "))
}

// annotationLines are the annotations on the event with eventID, evidence marked ▌ and comments │,
// each followed by its attachments.
func (m Model) annotationLines(eventID string) []string {
	annotations := m.annotations[eventID]
	if len(annotations) == 0 {
		return []string{m.fit(expandedIndent + m.styles.faint.Render("No annotations."))}
	}

	var lines []string
	for _, a := range annotations {
		mark, label, style := "▌", "evidence", m.styles.evidence
		if a.Kind == timeline.AnnotationComment {
			mark, label, style = "│", "comment", m.styles.comment
		}

		head := annotationHead(mark, label)
		under := strings.Repeat(" ", lipgloss.Width(head)-lipgloss.Width(mark))
		body := strings.Split(printable(a.Body), "\n")
		lines = append(lines, m.fit(expandedIndent+style.Render(head)+body[0]+m.styles.faint.Render(attribution(a))))
		for _, line := range body[1:] {
			lines = append(lines, m.fit(expandedIndent+style.Render(mark)+under+line))
		}
		lines = append(lines, m.attachmentLines(m.attachments[a.ID], expandedIndent+style.Render(mark)+under)...)
	}

	return lines
}

// annotationHead is an annotation's mark and kind, padded to where its body starts.
func annotationHead(mark, label string) string {
	return fmt.Sprintf("%s %-8s  ", mark, label)
}

// attachmentLines are attachments, each after prefix: an image sent to the terminal as its
// placeholder cells over its caption, anything else as its placeholder text.
func (m Model) attachmentLines(attachments []timeline.Attachment, prefix string) []string {
	var lines []string
	for _, a := range attachments {
		shown := m.images[a.ID]
		caption := graphics.Caption{
			Filename: a.Filename, Size: a.Size, Width: shown.img.Width, Height: shown.img.Height, Image: isImage(a),
		}
		if shown.id == 0 {
			lines = append(lines, m.fit(prefix+m.styles.faint.Render(caption.Placeholder())))
			continue
		}

		for _, cells := range graphics.Cells(shown.id, shown.grid) {
			lines = append(lines, m.fit(prefix+cells))
		}
		lines = append(lines, m.fit(prefix+m.styles.faint.Render(caption.String())))
	}

	return lines
}

// attachmentIndentWidth is how far in a's lines start: under its event, or under its
// annotation's head.
func attachmentIndentWidth(a timeline.Attachment) int {
	width := lipgloss.Width(expandedIndent)
	if a.Owner.AnnotationID != nil {
		width += lipgloss.Width(annotationHead("▌", "evidence"))
	}

	return width
}

func isImage(a timeline.Attachment) bool {
	return strings.HasPrefix(a.MIME, "image/")
}

func attribution(a timeline.Annotation) string {
	var by []string
	if a.Source != nil {
		by = append(by, "from "+*a.Source)
	}
	if a.Author != nil {
		by = append(by, "by "+oneLine(*a.Author))
	}
	if len(by) == 0 {
		return ""
	}

	return "  (" + strings.Join(by, ", ") + ")"
}

// printable is s with line endings made \n, tabs made four spaces and every other control
// character dropped, so stored text cannot move the cursor or send the terminal a sequence.
func printable(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// oneLine is s printable, with each run of white space made one space.
func oneLine(s string) string {
	return strings.Join(strings.Fields(printable(s)), " ")
}

// fit cuts line to the window's width.
func (m Model) fit(line string) string {
	return ansi.Truncate(line, m.width, "…")
}

func seqText(e timeline.Event) string {
	return "#" + strconv.FormatInt(e.Seq, 10)
}

func sourceText(e timeline.Event) string {
	if e.Source == nil {
		return "-"
	}

	return *e.Source
}

func tagsText(e timeline.Event) string {
	if len(e.Tags) == 0 {
		return "-"
	}

	return strings.Join(e.Tags, ",")
}

// wall is e's occurred at in loc, or in its own offset when its precision window does not line up
// in loc, so a coarse window is never re-cut in another zone.
func wall(e timeline.Event, loc *time.Location) time.Time {
	t := e.OccurredAt.In(loc)
	if e.Precision.Truncate(t).Equal(e.OccurredAt) {
		return t
	}

	offset := 0 // a nil offset means the window was cut in UTC
	if e.OccurredOffset != nil {
		offset = *e.OccurredOffset
	}

	return e.OccurredAt.In(time.FixedZone("", offset))
}

// eventTime is e's wall time to its precision, ~ when approximate, then its ended at when it has
// one, then the zone's abbreviation, or the offset when e's window does not line up in loc. A
// space stands in for ~ so times align.
func eventTime(e timeline.Event, loc *time.Location) string {
	start := wall(e, loc)
	text := " " + clock(start, e.Precision)
	if e.Approximate {
		text = "~" + clock(start, e.Precision)
	}

	if e.EndedAt != nil {
		end := e.EndedAt.In(start.Location())
		if end.Format(time.DateOnly) != start.Format(time.DateOnly) {
			text += "–" + end.Format("Jan 02 ") + clock(end, e.Precision)
		} else {
			text += "–" + clock(end, e.Precision)
		}
	}

	if start.Location() != loc {
		return text + " " + start.Format("-07:00")
	}

	return text + " " + start.Format("MST")
}

// clock is t's wall clock to precision, with dashes for the digits it does not know.
func clock(t time.Time, precision timeline.Precision) string {
	switch precision {
	case timeline.PrecisionDay:
		return "--:--"
	case timeline.PrecisionHour:
		return t.Format("15") + ":--"
	case timeline.PrecisionMinute:
		return t.Format("15:04")
	case timeline.PrecisionSecond:
		return t.Format(time.TimeOnly)
	case timeline.PrecisionMillisecond:
		return t.Format("15:04:05.000")
	case timeline.PrecisionMicrosecond:
		return t.Format("15:04:05.000000")
	case timeline.PrecisionNanosecond:
		return t.Format("15:04:05.000000000")
	}

	return t.Format(time.TimeOnly)
}

// formatGap renders d to the minute, as in 47m, 1h05m or 2d03h.
func formatGap(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d >= day:
		return fmt.Sprintf("%dd%02dh", d/day, d%day/time.Hour)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", d/time.Hour, d%time.Hour/time.Minute)
	}

	return fmt.Sprintf("%dm", d/time.Minute)
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}

	return noun + "s"
}
