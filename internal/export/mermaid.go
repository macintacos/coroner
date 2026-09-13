package export

import (
	"cmp"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// MermaidMaxTextSize is Mermaid's default maxTextSize: the longest diagram source, in UTF-16
// code units, it renders. Hosts such as Obsidian cannot raise it.
const MermaidMaxTextSize = 50_000

// mermaidTime is Mermaid's dateFormat YYYY-MM-DDTHH:mm:ss as a Go layout. The diagram uses
// only second precision, and no syntax newer than GitHub's lagging Mermaid renders.
const mermaidTime = "2006-01-02T15:04:05"

// WriteMermaid writes env's timeline to w as a Mermaid gantt diagram: a section per source in
// order of first appearance, then the events with none, each event a milestone at its moment
// or a bar over its interval, with times on zone's wall clock and zone named in the title. It
// fails, writing nothing, unless env holds one timeline.
func WriteMermaid(w io.Writer, env Envelope, zone *time.Location) error {
	diagram, err := mermaid(env, zone)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, diagram)
	return err
}

// MermaidSize returns the length of env's diagram as Mermaid measures it against
// MermaidMaxTextSize, in UTF-16 code units.
func MermaidSize(env Envelope, zone *time.Location) (int, error) {
	diagram, err := mermaid(env, zone)

	return len(utf16.Encode([]rune(diagram))), err
}

func mermaid(env Envelope, zone *time.Location) (string, error) {
	if len(env.Timelines) != 1 {
		return "", fmt.Errorf("want one timeline, got %d", len(env.Timelines))
	}

	gantt := mermaidRenderer{
		zone: cmp.Or(zone, time.UTC),
		// Mermaid decodes #<code>; entities in text, so # is escaped first, as its own entity.
		// A : ends a task label, and % starts a comment or a directive.
		escaper: strings.NewReplacer("#", "#35;", ":", "#58;", "%", "#37;"),
		// What Mermaid's case-insensitive gantt lexer matches before task text, at a label's start.
		keyword: regexp.MustCompile(`(?i)^(gantt|inclusiveenddates|topaxis|accdescr\s*\{|href\s+"|(call|click|dateformat|axisformat|tickinterval|includes|excludes|todaymarker|weekday|weekend|title|accdescription|section)\s|\d{4}-\d\d-\d\d)`),
	}
	t := env.Timelines[0]
	lines := []string{
		"gantt",
		"  title " + gantt.text(t.Name+" "+t.Title) + " (" + gantt.text(gantt.zone.String()) + ")",
		"  dateFormat YYYY-MM-DDTHH:mm:ss",
		"  axisFormat %H:%M:%S",
	}
	events := slices.SortedFunc(slices.Values(env.Events), compareEvents)
	for _, g := range groupEvents(events, sourceOf) {
		section := "(no source)"
		if g.key != "" {
			section = gantt.text(g.key)
		}
		lines = append(lines, "  section "+section)
		for _, e := range g.events {
			lines = append(lines, "    "+gantt.task(e))
		}
	}

	return strings.Join(lines, "\n") + "\n", nil
}

type mermaidRenderer struct {
	zone    *time.Location
	escaper *strings.Replacer
	keyword *regexp.Regexp
}

// task returns e's task line: a milestone at its start, or a bar to its end.
func (gantt mermaidRenderer) task(e Event) string {
	start := time.Time(e.OccurredAt).In(gantt.zone).Format(mermaidTime)
	if e.EndedAt == nil {
		return gantt.label(e) + " :milestone, " + start + ", 0s"
	}

	return gantt.label(e) + " :" + start + ", " + time.Time(*e.EndedAt).In(gantt.zone).Format(mermaidTime)
}

// label returns e's text as a task label, ~ first when it is approximate. A label that would
// start with gantt syntax has its first character written as an entity instead.
func (gantt mermaidRenderer) label(e Event) string {
	label := gantt.text(e.Text)
	if e.Approximate {
		return "~" + label
	}
	if gantt.keyword.MatchString(label) {
		first, size := utf8.DecodeRuneInString(label)
		return fmt.Sprintf("#%d;", first) + label[size:]
	}

	return label
}

// text returns s on one line, trimmed and escaped for Mermaid.
func (gantt mermaidRenderer) text(s string) string {
	return gantt.escaper.Replace(strings.TrimSpace(strings.Join(lines(s), " ")))
}
