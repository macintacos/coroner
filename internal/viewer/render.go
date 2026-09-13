package viewer

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/macintacos/coroner/internal/timeline"
)

const (
	// gapThreshold is the shortest silence between events that gets its own row.
	gapThreshold = 5 * time.Minute
	// chromeHeight is the lines the header and footer take from the window.
	chromeHeight   = 4
	maxSourceWidth = 16
	maxTagsWidth   = 24
	indent         = "  "
)

type styles struct {
	name, faint, time, source, tags, gap, day, evidence, comment, selected, failure lipgloss.Style
}

// newStyles uses only the 16 ANSI colors, so the terminal's own theme picks the shades, and never
// lets a color carry meaning a glyph or word does not also carry.
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
	}
}

// header names the timeline and the zone times show in, then counts the events the filter keeps.
func (m Model) header() string {
	t := m.snapshot.Timeline
	left := m.styles.name.Render(t.Name)
	if t.Title != "" {
		left += "  " + t.Title
	}
	zone := m.opts.Location.String()
	if zone == "Local" {
		zone = "local"
	}
	right := m.styles.faint.Render("times in " + zone)
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
	if len(m.filter.Sources) > 0 {
		kept = append(kept, "from "+strings.Join(m.filter.Sources, " or "))
	}
	if len(m.filter.Tags) > 0 {
		kept = append(kept, "tagged "+strings.Join(m.filter.Tags, " and "))
	}
	if m.filter.Contains != "" {
		kept = append(kept, fmt.Sprintf("containing %q", m.filter.Contains))
	}

	return strings.TrimSpace(count + " " + strings.Join(kept, ", "))
}

func (m Model) footer() string {
	line := m.styles.failure.Render(m.problem)
	if m.prompt != noPrompt {
		line = m.input.View()
	}

	h := m.help
	h.SetWidth(m.width)
	return m.fit(line) + "\n" + h.ShortHelpView(m.keys.ShortHelp())
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

// bodyLines returns the event list's lines, and the first and last line of the cursor event's
// block: the rules above it, its row and its annotations.
func (m Model) bodyLines() ([]string, int, int) {
	events := m.snapshot.Events
	if len(events) == 0 {
		return []string{indent + m.emptyMessage()}, 0, 0
	}

	gaps := map[int]time.Duration{}
	for _, g := range timeline.Gaps(events, gapThreshold) {
		gaps[g.Before] = g.Duration
	}
	widths := measure(events, m.opts.Location)

	var lines []string
	var top, bottom int
	day := ""
	for i, e := range events {
		start := len(lines)
		if d, ok := gaps[i]; ok {
			lines = append(lines, m.rule(formatGap(d), m.styles.gap))
		}
		if date := e.OccurredAt.In(m.opts.Location).Format("Mon 2006-01-02"); date != day {
			lines = append(lines, m.rule(date, m.styles.day))
			day = date
		}
		lines = append(lines, m.row(e, widths, i == m.cursor))
		if m.expanded[e.ID] {
			lines = append(lines, m.annotationLines(e.ID)...)
		}
		if i == m.cursor {
			top, bottom = start, len(lines)-1
		}
	}

	return lines, top, bottom
}

func (m Model) emptyMessage() string {
	if len(m.filter.Sources) > 0 || len(m.filter.Tags) > 0 || m.filter.Contains != "" {
		return "No events match. Press s, t or / to change the filter."
	}

	return "No events yet. Add one with: coroner event add <text>"
}

// rule is a full-width line carrying label, such as a gap or a date.
func (m Model) rule(label string, style lipgloss.Style) string {
	text := indent + "── " + label + " "
	return style.Render(text + strings.Repeat("─", max(m.width-lipgloss.Width(text), 2)))
}

// widths are the event list's column widths.
type widths struct {
	seq, time, source, tags int
}

func measure(events []timeline.Event, loc *time.Location) widths {
	var w widths
	for _, e := range events {
		w.seq = max(w.seq, len(seqText(e)))
		w.time = max(w.time, lipgloss.Width(eventTime(e, loc)))
		w.source = max(w.source, min(len(sourceText(e)), maxSourceWidth))
		w.tags = max(w.tags, min(len(tagsText(e)), maxTagsWidth))
	}

	return w
}

// row is e's line: seq, time, source, tags, text and how many annotations it has.
func (m Model) row(e timeline.Event, w widths, selected bool) string {
	fields := []string{
		pad(seqText(e), w.seq),
		pad(eventTime(e, m.opts.Location), w.time),
		pad(ansi.Truncate(sourceText(e), w.source, "…"), w.source),
		pad(ansi.Truncate(tagsText(e), w.tags, "…"), w.tags),
		strings.Join(strings.Fields(e.Text), " "),
	}
	count := ""
	if n := len(m.annotations[e.ID]); n > 0 {
		count = fmt.Sprintf("  (%d %s)", n, plural(n, "annotation"))
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

// annotationLines are the annotations on the event with eventID, evidence marked ▌ and comments │.
func (m Model) annotationLines(eventID string) []string {
	annotations := m.annotations[eventID]
	if len(annotations) == 0 {
		return []string{m.fit(indent + "    " + m.styles.faint.Render("No annotations."))}
	}

	var lines []string
	for _, a := range annotations {
		mark, label, style := "▌", "evidence", m.styles.evidence
		if a.Kind == timeline.AnnotationComment {
			mark, label, style = "│", "comment ", m.styles.comment
		}

		body := strings.Split(strings.ReplaceAll(a.Body, "\t", "    "), "\n")
		lines = append(lines, m.fit(indent+"    "+style.Render(mark+" "+label)+"  "+body[0]+m.styles.faint.Render(attribution(a))))
		for _, line := range body[1:] {
			lines = append(lines, m.fit(indent+"    "+style.Render(mark)+strings.Repeat(" ", len(label)+3)+line))
		}
	}

	return lines
}

func attribution(a timeline.Annotation) string {
	var by []string
	if a.Source != nil {
		by = append(by, "from "+*a.Source)
	}
	if a.Author != nil {
		by = append(by, "by "+*a.Author)
	}
	if len(by) == 0 {
		return ""
	}

	return "  (" + strings.Join(by, ", ") + ")"
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

// eventTime is e's occurred at in loc to its precision, ~ when approximate, then its ended at
// when it has one, then the zone's abbreviation. A space stands in for ~ so times align.
func eventTime(e timeline.Event, loc *time.Location) string {
	start := e.OccurredAt.In(loc)
	text := " " + clock(start, e.Precision)
	if e.Approximate {
		text = "~" + clock(start, e.Precision)
	}

	if e.EndedAt != nil {
		end := e.EndedAt.In(loc)
		if end.Format(time.DateOnly) != start.Format(time.DateOnly) {
			text += "–" + end.Format("Jan 02 ") + clock(end, e.Precision)
		} else {
			text += "–" + clock(end, e.Precision)
		}
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

// formatGap renders d in its two largest units, as in 45s, 47m, 1h05m or 2d03h.
func formatGap(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d >= day:
		return fmt.Sprintf("%dd%02dh", d/day, d%day/time.Hour)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", d/time.Hour, d%time.Hour/time.Minute)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", d/time.Minute)
	}

	return fmt.Sprintf("%ds", d/time.Second)
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
