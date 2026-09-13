package export

import (
	"cmp"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// MermaidMaxTextSize is Mermaid's default maxTextSize: the longest diagram source, in UTF-16
// code units, it renders. Hosts such as Obsidian cannot raise it.
const MermaidMaxTextSize = 50_000

const (
	mermaidDateFormat = "YYYY-MM-DDTHH:mm:ss"
	// mermaidTime is mermaidDateFormat as a Go layout.
	mermaidTime = "2006-01-02T15:04:05"
)

// WriteMermaid writes env's timeline to w as a Mermaid gantt diagram: a section per source in
// order of first appearance, then the events with none, each event a milestone at its moment
// or a bar for its interval's duration, with times on zone's wall clock to the second, a nil
// zone being UTC, and the title naming the zone by its abbreviations. It fails, writing
// nothing, unless env holds one timeline.
func WriteMermaid(w io.Writer, env Envelope, zone *time.Location) error {
	diagram, err := mermaid(env, zone)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, diagram)
	return err
}

// MermaidSize returns the length of env's diagram in zone, nil being UTC, as Mermaid measures
// it against MermaidMaxTextSize, in UTF-16 code units.
func MermaidSize(env Envelope, zone *time.Location) (int, error) {
	diagram, err := mermaid(env, zone)
	size := 0
	for _, r := range diagram {
		size += utf16.RuneLen(r)
	}

	return size, err
}

// mermaid returns env's diagram as WriteMermaid writes it.
func mermaid(env Envelope, zone *time.Location) (string, error) {
	if len(env.Timelines) != 1 {
		return "", fmt.Errorf("want one timeline, got %d", len(env.Timelines))
	}

	gantt := mermaidRenderer{
		zone: cmp.Or(zone, time.UTC),
		// Mermaid decodes #<code>; entities, so a literal # is written as one. A : ends a task
		// label, % starts a comment or directive, and Mermaid before 10.8.0 ends text at ;.
		escaper: strings.NewReplacer("#", "#35;", ":", "#58;", "%", "#37;", ";", "#59;"),
		// What Mermaid 11.12.0's gantt.jison matches before task text, \s being JavaScript's. It
		// runs on escaped text, so rules needing a raw : (accTitle:, accDescr:) are absent.
		syntax: regexp.MustCompile(strings.ReplaceAll(
			`(?i)^\s*(gantt|inclusiveenddates|topaxis|accdescr\s*\{|href\s+"|(call|click|dateformat|axisformat|tickinterval|includes|excludes|todaymarker|weekday|weekend|title|accdescription|section)\s|\d{4}-\d\d-\d\d)`,
			`\s`, `[\s\x{0B}\x{FEFF}\p{Z}]`,
		)),
	}
	events := slices.SortedFunc(slices.Values(env.Events), compareEvents)
	rows := slices.Concat(gantt.header(env.Timelines[0], events, env.ExportedAt), gantt.sections(events))

	return strings.Join(rows, "\n") + "\n", nil
}

// mermaidRenderer renders the lines of a Mermaid gantt diagram.
type mermaidRenderer struct {
	zone    *time.Location
	escaper *strings.Replacer
	syntax  *regexp.Regexp
}

// header returns the diagram's opening lines: t's title, naming the zone, and the date formats.
func (gantt mermaidRenderer) header(t Timeline, events []Event, exportedAt Instant) []string {
	return []string{
		"gantt",
		"  title " + gantt.text(t.Name+" "+t.Title) + " (" + gantt.text(gantt.zoneNames(events, exportedAt)) + ")",
		"  dateFormat " + mermaidDateFormat,
		"  axisFormat %H:%M:%S",
	}
}

// zoneNames returns the zone abbreviations of events' instants, in order of first appearance,
// joined by /, or the one at exportedAt when there are no events.
func (gantt mermaidRenderer) zoneNames(events []Event, exportedAt Instant) string {
	var names []string
	for _, e := range events {
		for _, t := range eventInstants(e) {
			if name := zoneName(t.In(gantt.zone)); !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return zoneName(time.Time(exportedAt).In(gantt.zone))
	}

	return strings.Join(names, "/")
}

// sections returns a section of tasks per source of events, in order of first appearance, then
// one of the events with none.
func (gantt mermaidRenderer) sections(events []Event) []string {
	var rows []string
	for _, g := range groupEvents(events, sourceOf) {
		section := "(no source)"
		if g.key != "" {
			section = gantt.text(g.key)
		}
		rows = append(rows, "  section "+section)
		for _, e := range g.events {
			rows = append(rows, "    "+gantt.task(e))
		}
	}

	return rows
}

// task returns e's task line: a milestone at its start, or a bar for its duration. A duration,
// unlike an end date, cannot run backwards where the wall clock repeats an hour.
func (gantt mermaidRenderer) task(e Event) string {
	start := time.Time(e.OccurredAt).Truncate(time.Second)
	at := start.In(gantt.zone).Format(mermaidTime)
	if e.EndedAt == nil {
		return gantt.label(e) + " :milestone, " + at + ", 0s"
	}

	seconds := time.Time(*e.EndedAt).Truncate(time.Second).Sub(start) / time.Second
	return gantt.label(e) + " :" + at + ", " + strconv.FormatInt(int64(seconds), 10) + "s"
}

// label returns e's text as a task label, ~ first when it is approximate. Otherwise, a label
// that would start with gantt syntax has its first character written as an entity.
func (gantt mermaidRenderer) label(e Event) string {
	label := gantt.text(e.Text)
	if e.Approximate {
		return "~" + label
	}
	if gantt.syntax.MatchString(label) {
		first, size := utf8.DecodeRuneInString(label)
		return fmt.Sprintf("#%d;", first) + label[size:]
	}

	return label
}

// text returns s on one line, trimmed and escaped for Mermaid.
func (gantt mermaidRenderer) text(s string) string {
	return gantt.escaper.Replace(strings.TrimSpace(strings.Join(lines(s), " ")))
}
