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

// ParseGroupBy returns the grouping name selects, "" selecting GroupByDay, and false when
// name selects none.
func ParseGroupBy(name string) (GroupBy, bool) {
	switch g := GroupBy(name); g {
	case GroupByDay, GroupBySource, GroupByTag:
		return g, true
	}

	return GroupByDay, false
}

// MarkdownOptions are how a Markdown export presents its timeline.
type MarkdownOptions struct {
	// Zone is where times and days are shown, each labelled with the zone's abbreviation at
	// that instant; nil is UTC.
	Zone    *time.Location
	GroupBy GroupBy
	// IncludeRaw adds each event's time as it was written.
	IncludeRaw bool
	// Diagram appends the timeline's Mermaid diagram, in Zone, as a mermaid code block.
	Diagram bool
}

// WriteMarkdown writes env's timeline to w as a CommonMark outline for people: a title,
// metadata bullets, the annotations on the whole timeline, then a section per day of its
// events by occurred at and seq, with each event's annotations nested under it. It fails,
// writing nothing, unless env holds one timeline and the event of every annotation, and
// opts a grouping ParseGroupBy knows.
func WriteMarkdown(w io.Writer, env Envelope, opts MarkdownOptions) error {
	if len(env.Timelines) != 1 {
		return fmt.Errorf("want one timeline, got %d", len(env.Timelines))
	}
	if _, ok := ParseGroupBy(string(opts.GroupBy)); !ok {
		return fmt.Errorf("unknown grouping %q", opts.GroupBy)
	}
	events := slices.SortedFunc(slices.Values(env.Events), compareEvents)
	onTimeline, byEvent, err := groupAnnotations(env.Annotations, events)
	if err != nil {
		return err
	}

	md := markdownRenderer{
		zone:       cmp.Or(opts.Zone, time.UTC),
		groupBy:    opts.GroupBy,
		includeRaw: opts.IncludeRaw,
		byEvent:    byEvent,
		// Escapes what opens inline syntax (emphasis, code, links, HTML, entities, table cells,
		// strikethrough) so text shows as written; & keeps &lt; literal. escape leaves URLs whole.
		escaper: strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "|", `\|`, "&", `\&`, "~", `\~`),
		urls:    regexp.MustCompile("https?://[^\\s<>`]+"),
	}
	blocks := slices.Concat(
		md.header(env.Timelines[0], events, md.exported(env.ExportedAt, env.CoronerVersion)),
		md.timelineAnnotations(onTimeline),
		md.sections(events),
	)
	if opts.Diagram {
		diagram, err := mermaid(env, md.zone)
		if err != nil {
			return err
		}
		// No diagram line can close the fence: a closing fence holds only backticks, and every line holds more.
		blocks = append(blocks, "```mermaid\n"+diagram+"```")
	}

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

// markdownRenderer renders the blocks of a Markdown export. Each block is a heading, paragraph
// or list without a trailing newline, for the caller to separate by a blank line.
type markdownRenderer struct {
	zone       *time.Location
	groupBy    GroupBy
	includeRaw bool
	byEvent    map[string][]Annotation
	escaper    *strings.Replacer
	urls       *regexp.Regexp
}

// header returns t's title and the metadata list: refs, the window events span, the event
// summary, then exported.
func (md markdownRenderer) header(t Timeline, events []Event, exported string) []string {
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
	metadata = append(metadata, eventSummary(events), exported)

	return []string{title, strings.Join(metadata, "\n")}
}

// exported returns the metadata bullet saying when and by which coroner the export was written.
func (md markdownRenderer) exported(at Instant, coronerVersion string) string {
	local := time.Time(at).In(md.zone)

	return "- **Exported:** " + local.Format("2006-01-02 15:04") + " " + zoneName(local) + " by coroner " + md.oneLine(coronerVersion)
}

// window returns the span from the first event to the last moment any event reaches, to the
// second, labelled once when both ends share a zone abbreviation.
func (md markdownRenderer) window(events []Event) string {
	var instants []time.Time
	for _, e := range events {
		instants = append(instants, eventInstants(e)...)
	}
	start := time.Time(events[0].OccurredAt).In(md.zone)
	end := slices.MaxFunc(instants, time.Time.Compare).In(md.zone)

	startText := start.Format(time.DateTime)
	if zoneName(start) != zoneName(end) {
		startText += " " + zoneName(start)
	}
	endLayout := time.TimeOnly
	if end.Format(time.DateOnly) != start.Format(time.DateOnly) {
		endLayout = time.DateTime
	}

	return startText + " → " + end.Format(endLayout) + " " + zoneName(end)
}

// eventSummary returns the metadata bullet counting events, with the sources and tags they
// name, by name.
func eventSummary(events []Event) string {
	var sources, tags []string
	for _, e := range events {
		if e.Source != nil {
			sources = append(sources, *e.Source)
		}
		tags = append(tags, e.Tags...)
	}
	slices.Sort(sources)
	slices.Sort(tags)

	text := "- **Events:** " + strconv.Itoa(len(events))
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
func (md markdownRenderer) timelineAnnotations(annotations []Annotation) []string {
	if len(annotations) == 0 {
		return nil
	}

	return []string{"Annotations on the whole timeline:", md.annotationList(annotations, "")}
}

// sections returns the event sections md.groupBy asks for.
func (md markdownRenderer) sections(events []Event) []string {
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
func (md markdownRenderer) daySections(events []Event, level, groupTag string) []string {
	var blocks []string
	for _, day := range md.days(events) {
		label, offset := md.dayZone(day)
		items := make([]string, 0, len(day))
		for _, e := range day {
			items = append(items, md.eventItem(e, offset, groupTag))
		}
		blocks = append(blocks,
			level+" "+md.date(day[0])+" ("+label+")",
			list(items),
		)
	}

	return blocks
}

// days splits events, in occurred at and seq order, by the day each belongs to, with
// day-precision events first within their day.
func (md markdownRenderer) days(events []Event) [][]Event {
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
func (md markdownRenderer) date(e Event) string {
	at := time.Time(e.OccurredAt)
	if e.Precision == timeline.PrecisionDay {
		return at.In(time.FixedZone("", deref(e.OccurredOffset))).Format(time.DateOnly)
	}

	return at.In(md.zone).Format(time.DateOnly)
}

// dayZone returns the zone abbreviations a day's times are shown in, in order, joined by /,
// and whether they span more than one offset, so that every time on the day needs its own.
func (md markdownRenderer) dayZone(day []Event) (string, bool) {
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
// annotations. offset adds the zone offset to every time. In the section of groupTag, the
// item names only e's other tags, as (also: …).
func (md markdownRenderer) eventItem(e Event, offset bool, groupTag string) string {
	var parts []string
	if when := md.when(e, offset); when != "" {
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
// approximate. The end carries its date when that differs from the start's. A day-precision
// event has dayWhen's instead.
func (md markdownRenderer) when(e Event, offset bool) string {
	if e.Precision == timeline.PrecisionDay {
		return md.dayWhen(e)
	}

	start := time.Time(e.OccurredAt).In(md.zone)
	when := clock(start, e.Precision, offset)
	if e.EndedAt != nil {
		end := time.Time(*e.EndedAt).In(md.zone)
		endText := clock(end, e.Precision, offset)
		if end.Format(time.DateOnly) != start.Format(time.DateOnly) {
			endText = end.Format(time.DateOnly) + " " + endText
		}
		when += "–" + endText
	}
	if e.Approximate {
		when = "~" + when
	}

	return when
}

// dayWhen returns what a day-precision event's section heading leaves unsaid: approximately
// when it is approximate, and the date in the zone its interval runs until.
func (md markdownRenderer) dayWhen(e Event) string {
	var parts []string
	if e.Approximate {
		parts = append(parts, "approximately")
	}
	if e.EndedAt != nil {
		parts = append(parts, "until "+time.Time(*e.EndedAt).In(md.zone).Format(time.DateOnly))
	}

	return strings.Join(parts, ", ")
}

// annotationList returns the list items of annotations, each indented by indent.
func (md markdownRenderer) annotationList(annotations []Annotation, indent string) string {
	items := make([]string, 0, len(annotations))
	for _, a := range annotations {
		items = append(items, md.annotationItem(a, indent))
	}

	return list(items)
}

// list joins items into one list, with a blank line between items when any item holds
// one: the list is loose then anyway, and a fence needs blank lines around it.
func list(items []string) string {
	sep := "\n"
	if slices.ContainsFunc(items, func(item string) bool { return strings.Contains(item, "\n\n") }) {
		sep = "\n\n"
	}

	return strings.Join(items, sep)
}

// annotationItem returns a's list item: evidence as code, anything else as emphasized
// commentary.
func (md markdownRenderer) annotationItem(a Annotation, indent string) string {
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

	return indent + "- evidence" + by + ":\n\n" + codeBlock(body, indent+"  ")
}

// text escapes s as CommonMark text that may start a line, its line breaks as hard breaks
// followed by indent.
func (md markdownRenderer) text(s, indent string) string {
	escaped := lines(s)
	for i, line := range escaped {
		escaped[i] = escapeLineStart(md.escape(line))
	}

	return strings.Join(escaped, "\\\n"+indent)
}

// oneLine escapes s as CommonMark text inside a line, with its line breaks as spaces.
func (md markdownRenderer) oneLine(s string) string {
	return md.escape(strings.Join(lines(s), " "))
}

// escape escapes s with md.escaper, but for its http and https URLs, which it keeps as
// written: GFM ends an autolink at a backslash.
func (md markdownRenderer) escape(s string) string {
	var b strings.Builder
	last := 0
	for _, match := range md.urls.FindAllStringIndex(s, -1) {
		b.WriteString(md.escaper.Replace(s[last:match[0]]))
		b.WriteString(s[match[0]:match[1]])
		last = match[1]
	}
	b.WriteString(md.escaper.Replace(s[last:]))

	return b.String()
}

// escapeLineStart backslash-escapes the character that would make line start a block.
func escapeLineStart(line string) string {
	start := len(line) - len(strings.TrimLeft(line, " \t"))
	if marker := blockMarker(line[start:]); marker >= 0 {
		return line[:start+marker] + `\` + line[start+marker:]
	}

	return line
}

// blockMarker returns the index of the character that would make line start a heading, list
// item or setext underline, or -1. *, _, >, ~ and backticks need no check: the escaper escapes
// them everywhere.
func blockMarker(line string) int {
	if line != "" && strings.ContainsRune("#-+=", rune(line[0])) {
		return 0
	}
	digits := len(line) - len(strings.TrimLeft(line, "0123456789"))
	if digits > 0 && digits < len(line) && (line[digits] == '.' || line[digits] == ')') {
		return digits
	}

	return -1
}

// clock returns t's wall clock to precision, and its offset when offset is set; a day has none.
// An hour shows its minutes when the zone puts its start off the hour.
func clock(t time.Time, precision timeline.Precision, offset bool) string {
	layout := "15:04:05.000000000"
	switch precision {
	case timeline.PrecisionDay:
		return ""
	case timeline.PrecisionHour:
		layout = "15:--"
		if t.Minute() != 0 {
			layout = "15:04"
		}
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

// codeBlock returns body as a fenced code block indented by indent. Blank lines stay empty, so
// no line ends in spaces.
func codeBlock(body []string, indent string) string {
	fence := strings.Repeat("`", max(3, longestRun(strings.Join(body, "\n"), '`')+1))
	block := []string{indent + fence + "text"}
	for _, line := range body {
		if line != "" {
			line = indent + line
		}
		block = append(block, line)
	}

	return strings.Join(append(block, indent+fence), "\n")
}

// lines returns s split at its line breaks, of any convention, without trailing blank lines.
func lines(s string) []string {
	split := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s), "\n")
	last := len(split) - 1
	for last > 0 && strings.TrimSpace(split[last]) == "" {
		last--
	}

	return split[:last+1]
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

func zoneName(t time.Time) string {
	name, _ := t.Zone()
	return name
}
