package export

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
)

// GroupBy is how a Markdown export sections its events.
type GroupBy string

// Groupings. Every one puts a day's events in their own section.
const (
	// GroupByDay sections events by day alone.
	GroupByDay GroupBy = ""
	// GroupBySource sections events by source, in order of first appearance, then day.
	GroupBySource GroupBy = "source"
	// GroupByTag sections events under each of their tags, in order of first appearance, then
	// day.
	GroupByTag GroupBy = "tag"
)

// MarkdownOptions are how a Markdown export presents its timeline.
type MarkdownOptions struct {
	// Zone is where times and days are shown, each labelled with the zone's abbreviation at
	// that instant; nil is UTC.
	Zone    *time.Location
	GroupBy GroupBy
	// IncludeRaw adds each event's time as it was written.
	IncludeRaw bool
}

// WriteMarkdown writes env's timeline to w as a CommonMark outline for people: a title,
// metadata bullets, the annotations on the whole timeline, then a section per day of its
// events by occurred at and seq, with each event's annotations nested under it. It fails,
// writing nothing, unless env holds one timeline and the event of every annotation.
func WriteMarkdown(w io.Writer, env Envelope, opts MarkdownOptions) error {
	if len(env.Timelines) != 1 {
		return fmt.Errorf("markdown export: want one timeline, got %d", len(env.Timelines))
	}
	if !slices.Contains([]GroupBy{GroupByDay, GroupBySource, GroupByTag}, opts.GroupBy) {
		return fmt.Errorf("markdown export: unknown grouping %q", opts.GroupBy)
	}
	events := slices.SortedFunc(slices.Values(env.Events), compareEvents)
	onTimeline, byEvent, err := groupAnnotations(env.Annotations, events)
	if err != nil {
		return err
	}

	md := markdownWriter{
		zone:       cmp.Or(opts.Zone, time.UTC),
		groupBy:    opts.GroupBy,
		includeRaw: opts.IncludeRaw,
		byEvent:    byEvent,
		escaper:    strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "|", `\|`, "&", `\&`),
	}
	blocks := slices.Concat(md.header(env, events), md.timelineAnnotations(onTimeline), md.sections(events))

	_, err = io.WriteString(w, strings.Join(blocks, "\n\n")+"\n")
	return err
}

// groupAnnotations returns annotations by created at then id, split into those on no event
// and those on each event, and fails naming one whose event is not in events.
func groupAnnotations(annotations []Annotation, events []Event) ([]Annotation, map[string][]Annotation, error) {
	exported := map[string]bool{}
	for _, e := range events {
		exported[e.ID] = true
	}

	var onTimeline []Annotation
	byEvent := map[string][]Annotation{}
	for _, a := range slices.SortedFunc(slices.Values(annotations), compareAnnotations) {
		switch {
		case a.EventID == nil:
			onTimeline = append(onTimeline, a)
		case exported[*a.EventID]:
			byEvent[*a.EventID] = append(byEvent[*a.EventID], a)
		default:
			return nil, nil, fmt.Errorf("annotation %s: its event %s is not in the export", a.ID, *a.EventID)
		}
	}

	return onTimeline, byEvent, nil
}

// markdownWriter renders the blocks of a Markdown export. Each block is a heading, paragraph
// or list without a trailing newline, and blocks are separated by a blank line.
type markdownWriter struct {
	zone       *time.Location
	groupBy    GroupBy
	includeRaw bool
	byEvent    map[string][]Annotation
	escaper    *strings.Replacer
}

// header returns the title and the metadata list: refs, the window events span, counts, and
// when and by which coroner the export was written.
func (md markdownWriter) header(env Envelope, events []Event) []string {
	t := env.Timelines[0]
	title := "# " + md.oneLine(t.Name)
	if strings.TrimSpace(t.Title) != "" {
		title = "# " + md.oneLine(t.Title) + " (" + md.oneLine(t.Name) + ")"
	}

	var metadata []string
	if len(t.Refs) > 0 {
		refs := make([]string, 0, len(t.Refs))
		for _, ref := range t.Refs {
			refs = append(refs, md.oneLine(ref))
		}
		metadata = append(metadata, "- **Refs:** "+strings.Join(refs, ", "))
	}
	if len(events) > 0 {
		metadata = append(metadata, "- **Window:** "+md.window(events))
	}
	exportedAt := time.Time(env.ExportedAt).In(md.zone)
	metadata = append(metadata,
		"- **Events:** "+counts(events),
		"- **Exported:** "+exportedAt.Format("2006-01-02 15:04")+" "+zoneName(exportedAt)+" by coroner "+md.oneLine(env.CoronerVersion),
	)

	return []string{title, strings.Join(metadata, "\n")}
}

// window returns the span from the first event to the last moment any event reaches, to the
// second, labelled once when both ends share a zone abbreviation.
func (md markdownWriter) window(events []Event) string {
	start := time.Time(events[0].OccurredAt).In(md.zone)
	end := start
	for _, e := range events {
		end = latest(end, time.Time(e.OccurredAt).In(md.zone))
		if e.EndedAt != nil {
			end = latest(end, time.Time(*e.EndedAt).In(md.zone))
		}
	}

	text := start.Format(time.DateTime)
	if zoneName(start) != zoneName(end) {
		text += " " + zoneName(start)
	}
	endLayout := time.TimeOnly
	if end.Format(time.DateOnly) != start.Format(time.DateOnly) {
		endLayout = time.DateTime
	}

	return text + " → " + end.Format(endLayout) + " " + zoneName(end)
}

// counts returns how many events there are, and the sources and tags they name, by name.
func counts(events []Event) string {
	var sources, tags []string
	for _, e := range events {
		if e.Source != nil {
			sources = append(sources, *e.Source)
		}
		tags = append(tags, e.Tags...)
	}
	slices.Sort(sources)
	slices.Sort(tags)

	text := strconv.Itoa(len(events))
	if sources = slices.Compact(sources); len(sources) > 0 {
		text += " · **Sources:** " + codeSpans(sources, "", ", ")
	}
	if tags = slices.Compact(tags); len(tags) > 0 {
		text += " · **Tags:** " + codeSpans(tags, "#", " ")
	}

	return text
}

// timelineAnnotations returns a lead-in paragraph and the list of annotations on no event, or
// nothing when there are none. The paragraph keeps the list from merging into the metadata.
func (md markdownWriter) timelineAnnotations(annotations []Annotation) []string {
	if len(annotations) == 0 {
		return nil
	}

	return []string{"Annotations on the whole timeline:", strings.TrimSuffix(md.annotationList(annotations, ""), "\n")}
}

// sections returns the event sections md.groupBy asks for.
func (md markdownWriter) sections(events []Event) []string {
	var blocks []string
	switch md.groupBy {
	case GroupBySource:
		for _, g := range groupEvents(events, sourceOf) {
			heading := "(no source)"
			if g.key != "" {
				heading = codeSpan(g.key)
			}
			blocks = append(blocks, "## "+heading)
			blocks = append(blocks, md.daySections(g.events, "###", "")...)
		}
	case GroupByTag:
		for _, g := range groupEvents(events, tagsOf) {
			heading := "(untagged)"
			if g.key != "" {
				heading = codeSpan("#" + g.key)
			}
			blocks = append(blocks, "## "+heading)
			blocks = append(blocks, md.daySections(g.events, "###", g.key)...)
		}
	case GroupByDay:
		blocks = md.daySections(events, "##", "")
	}

	return blocks
}

// eventGroup is the events that share a key, such as a source; the group whose events have
// none has the key "".
type eventGroup struct {
	key    string
	events []Event
}

// groupEvents returns events under each key keys gives them, in order of first appearance,
// then the events keys gives none.
func groupEvents(events []Event, keys func(Event) []string) []eventGroup {
	var groups []eventGroup
	var keyless []Event
	index := map[string]int{}
	for _, e := range events {
		if len(keys(e)) == 0 {
			keyless = append(keyless, e)
		}
		for _, key := range keys(e) {
			i, ok := index[key]
			if !ok {
				i = len(groups)
				index[key] = i
				groups = append(groups, eventGroup{key: key})
			}
			groups[i].events = append(groups[i].events, e)
		}
	}
	if len(keyless) > 0 {
		groups = append(groups, eventGroup{events: keyless})
	}

	return groups
}

func sourceOf(e Event) []string {
	if e.Source == nil {
		return nil
	}

	return []string{*e.Source}
}

func tagsOf(e Event) []string {
	return e.Tags
}

// daySections returns a heading at level, such as ##, and an event list for each day events
// fall on, in date order. Events in the section of groupTag list their other tags instead of
// all of them.
func (md markdownWriter) daySections(events []Event, level, groupTag string) []string {
	var blocks []string
	for _, day := range md.days(events) {
		label, offsets := md.dayZone(day)
		items := make([]string, 0, len(day))
		for _, e := range day {
			items = append(items, md.eventItem(e, offsets, groupTag))
		}
		blocks = append(blocks,
			level+" "+md.date(day[0])+" ("+label+")",
			strings.TrimSuffix(strings.Join(items, "\n"), "\n"),
		)
	}

	return blocks
}

// days splits events, in occurred at and seq order, by the day each belongs to, with
// day-precision events first within their day.
func (md markdownWriter) days(events []Event) [][]Event {
	sorted := slices.Clone(events)
	slices.SortStableFunc(sorted, func(a, b Event) int {
		return cmp.Or(cmp.Compare(md.date(a), md.date(b)), cmp.Compare(timed(a), timed(b)))
	})

	var days [][]Event
	for i, e := range sorted {
		if i == 0 || md.date(e) != md.date(sorted[i-1]) {
			days = append(days, nil)
		}
		days[len(days)-1] = append(days[len(days)-1], e)
	}

	return days
}

// date returns the day e belongs to: its date in the zone, or the date a day-precision event
// states, read in its own offset.
func (md markdownWriter) date(e Event) string {
	at := time.Time(e.OccurredAt)
	if e.Precision == timeline.PrecisionDay {
		return at.In(time.FixedZone("", deref(e.OccurredOffset))).Format(time.DateOnly)
	}

	return at.In(md.zone).Format(time.DateOnly)
}

// dayZone returns the zone abbreviations a day's times are shown in, in order, joined by /,
// and whether they span more than one offset, so that every time on the day needs its own.
func (md markdownWriter) dayZone(day []Event) (string, bool) {
	var names []string
	offsets := map[int]bool{}
	for _, e := range day {
		if e.Precision == timeline.PrecisionDay {
			continue
		}
		for _, t := range eventInstants(e) {
			name, offset := t.In(md.zone).Zone()
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
			offsets[offset] = true
		}
	}
	if len(names) == 0 {
		return zoneName(time.Time(day[0].OccurredAt).In(md.zone)), false
	}

	return strings.Join(names, "/"), len(offsets) > 1
}

// eventItem returns e's list item: its time, source, text and tags, and under it e's
// annotations. offsets adds the zone offset to every time. In the section of groupTag, the
// item names only e's other tags, as (also: …).
func (md markdownWriter) eventItem(e Event, offsets bool, groupTag string) string {
	var parts []string
	if when := md.when(e, offsets); when != "" {
		parts = append(parts, "**"+when+"**")
	}
	if e.Source != nil {
		parts = append(parts, codeSpan(*e.Source))
	}
	what := md.text(e.Text, "  ")
	otherTags := slices.DeleteFunc(slices.Clone(e.Tags), func(tag string) bool { return tag == groupTag })
	switch {
	case len(otherTags) == 0:
	case groupTag == "":
		what += " " + codeSpans(otherTags, "#", " ")
	default:
		what += " (also: " + codeSpans(otherTags, "#", " ") + ")"
	}
	parts = append(parts, what)
	if md.includeRaw && e.OccurredRaw != nil {
		parts = append(parts, "as written: "+codeSpan(strings.Join(lines(*e.OccurredRaw), " ")))
	}

	item := "- " + strings.Join(parts, " · ")
	if annotations := md.byEvent[e.ID]; len(annotations) > 0 {
		item += "\n" + md.annotationList(annotations, "  ")
	}

	return item
}

// when returns e's time at its precision in the zone, its interval's end, and ~ when it is
// approximate. The end carries its date when that differs from the start's.
func (md markdownWriter) when(e Event, offsets bool) string {
	start := time.Time(e.OccurredAt).In(md.zone)
	when := clock(start, e.Precision, offsets)
	if e.EndedAt != nil {
		end := time.Time(*e.EndedAt).In(md.zone)
		endText := clock(end, e.Precision, offsets)
		if endText == "" || end.Format(time.DateOnly) != start.Format(time.DateOnly) {
			endText = strings.TrimSpace(end.Format(time.DateOnly) + " " + endText)
		}
		when += "–" + endText
	}
	if e.Approximate {
		when = "~" + when
	}

	return when
}

// annotationList returns the list items of annotations, each indented by indent. An item that
// ends in a code block ends in a newline, so a blank line follows it.
func (md markdownWriter) annotationList(annotations []Annotation, indent string) string {
	items := make([]string, 0, len(annotations))
	for _, a := range annotations {
		items = append(items, md.annotationItem(a, indent))
	}

	return strings.Join(items, "\n")
}

// annotationItem returns a's list item: evidence as code, anything else as emphasized
// commentary.
func (md markdownWriter) annotationItem(a Annotation, indent string) string {
	by := ""
	if a.Author != nil {
		by = " (" + md.oneLine(*a.Author) + ")"
	}
	if a.Kind != timeline.AnnotationEvidence {
		return indent + "- _" + md.oneLine(string(a.Kind)) + by + ":_ " + md.text(a.Body, indent+"  ")
	}

	body := lines(a.Body)
	if len(body) == 1 {
		return indent + "- evidence" + by + ": " + codeSpan(body[0])
	}

	return indent + "- evidence" + by + ":\n\n" + codeBlock(body, indent+"  ") + "\n"
}

// text escapes s as CommonMark text that may start a line, its line breaks as hard breaks
// followed by indent.
func (md markdownWriter) text(s, indent string) string {
	escaped := lines(s)
	for i, line := range escaped {
		escaped[i] = md.escapeLineStart(md.escaper.Replace(line))
	}

	return strings.Join(escaped, "\\\n"+indent)
}

// oneLine escapes s as CommonMark text inside a line, with its line breaks as spaces.
func (md markdownWriter) oneLine(s string) string {
	return md.escaper.Replace(strings.Join(lines(s), " "))
}

// escapeLineStart backslash-escapes the character that would make line start a block.
func (markdownWriter) escapeLineStart(line string) string {
	start := len(line) - len(strings.TrimLeft(line, " \t"))
	if marker := blockMarker(line[start:]); marker >= 0 {
		return line[:start+marker] + `\` + line[start+marker:]
	}

	return line
}

// blockMarker returns the index of the character that would make line start a heading, list
// item, fence or setext underline, or -1.
func blockMarker(line string) int {
	if line != "" && strings.ContainsRune("#-+=~", rune(line[0])) {
		return 0
	}
	digits := len(line) - len(strings.TrimLeft(line, "0123456789"))
	if digits > 0 && digits < len(line) && (line[digits] == '.' || line[digits] == ')') {
		return digits
	}

	return -1
}

// clock returns t's wall clock to precision, and its offset when offset is set; a day has none.
func clock(t time.Time, precision timeline.Precision, offset bool) string {
	layout := "15:04:05.000000000"
	switch precision {
	case timeline.PrecisionDay:
		return ""
	case timeline.PrecisionHour:
		layout = "15h"
	case timeline.PrecisionMinute:
		layout = "15:04"
	case timeline.PrecisionSecond:
		layout = time.TimeOnly
	case timeline.PrecisionMillisecond:
		layout = "15:04:05.000"
	case timeline.PrecisionMicrosecond:
		layout = "15:04:05.000000"
	case timeline.PrecisionNanosecond:
	}
	if offset {
		layout += " -07:00"
	}

	return t.Format(layout)
}

// codeSpans returns each name, after prefix, as a code span, joined by sep.
func codeSpans(names []string, prefix, sep string) string {
	spans := make([]string, 0, len(names))
	for _, name := range names {
		spans = append(spans, codeSpan(prefix+name))
	}

	return strings.Join(spans, sep)
}

// codeSpan returns s as a code span, fenced by more backticks than it holds in a row, and
// padded so CommonMark strips nothing of a leading or trailing backtick or space.
func codeSpan(s string) string {
	fence := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		s = " " + s + " "
	}

	return fence + s + fence
}

// codeBlock returns lines as a fenced code block indented by indent.
func codeBlock(lines []string, indent string) string {
	fence := strings.Repeat("`", max(3, longestRun(strings.Join(lines, "\n"), '`')+1))
	block := []string{indent + fence + "text"}
	for _, line := range lines {
		if line != "" {
			line = indent + line
		}
		block = append(block, line)
	}

	return strings.Join(append(block, indent+fence), "\n")
}

// lines returns s split at its line breaks, of any convention, without trailing empty lines.
func lines(s string) []string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)

	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func longestRun(s string, c byte) int {
	longest, run := 0, 0
	for i := range len(s) {
		run++
		if s[i] != c {
			run = 0
		}
		longest = max(longest, run)
	}

	return longest
}

func eventInstants(e Event) []time.Time {
	if e.EndedAt == nil {
		return []time.Time{time.Time(e.OccurredAt)}
	}

	return []time.Time{time.Time(e.OccurredAt), time.Time(*e.EndedAt)}
}

// timed orders day-precision events before timed ones.
func timed(e Event) int {
	if e.Precision == timeline.PrecisionDay {
		return 0
	}

	return 1
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

func zoneName(t time.Time) string {
	name, _ := t.Zone()
	return name
}
