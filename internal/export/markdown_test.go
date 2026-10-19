package export_test

import (
	"bufio"
	"bytes"
	stdhtml "html"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

func utc(value string) export.Instant {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		panic(err)
	}

	return export.Instant(t)
}

func markdown(env export.Envelope, opts export.MarkdownOptions) string {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(export.WriteMarkdown(&b, env, opts)).To(Succeed())

	return b.String()
}

func envelope(t export.Timeline, events []export.Event, annotations []export.Annotation) export.Envelope {
	env := export.NewEnvelope("0.1.0", time.Date(2026, time.September, 12, 21, 40, 5, 0, time.UTC))
	env.Timelines = []export.Timeline{t}
	for i := range events {
		if events[i].TimelineID == "" {
			events[i].TimelineID = t.ID
		}
	}
	for i := range annotations {
		if annotations[i].TimelineID == "" {
			annotations[i].TimelineID = t.ID
		}
	}
	env.Events = events
	env.Annotations = annotations

	return env
}

// checkoutSpike is a timeline over two days: sourced and unsourced events, tags, an
// interval, annotations on the timeline and on events, one of them multi-line, and
// attachments, images and not, on an event and on annotations of each shape.
func checkoutSpike() export.Envelope {
	env := envelope(
		export.Timeline{
			ID: "t1", Name: "checkout-spike", Title: "Checkout latency spike",
			Refs: []string{"INC-4821", "https://status.example.com/incidents/4821"},
		},
		[]export.Event{
			{
				ID: "e3", Seq: new(int64(3)), OccurredAt: utc("2026-09-11T00:02:30Z"), Precision: timeline.PrecisionSecond,
				EndedAt: new(utc("2026-09-11T00:05:40Z")), Source: new("mongod"), Tags: []string{"db", "election"},
				Text: "Replica set has no primary",
			},
			{
				ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-09-10T23:58:12Z"), Precision: timeline.PrecisionSecond,
				Source: new("slack"), Tags: []string{}, Text: "maria: checkout p99 is 4s",
			},
			{
				ID: "e2", Seq: new(int64(2)), OccurredAt: utc("2026-09-10T23:59:00Z"), Precision: timeline.PrecisionMinute,
				Approximate: true, Source: new("mongod"), Tags: []string{"db"}, Text: "Slow query on checkout.orders: 2150 ms",
			},
			{
				ID: "e4", Seq: new(int64(4)), OccurredAt: utc("2026-09-11T00:07:00Z"), Precision: timeline.PrecisionMinute,
				Tags: []string{}, Text: "Rolled back deploy 812",
			},
		},
		[]export.Annotation{
			{
				ID: "a5", EventID: new("e3"), Kind: timeline.AnnotationEvidence, Author: new("julian"),
				Body: "rs.status()\n{\n  \"ok\": 0,\n  \"errmsg\": \"no primary\"\n}", CreatedAt: utc("2026-09-12T10:00:00Z"),
			},
			{
				ID: "a1", Kind: timeline.AnnotationComment, Author: new("sam"), Body: "Started from the checkout alert.",
				CreatedAt: utc("2026-09-12T09:00:00Z"),
			},
			{
				ID: "a2", Kind: timeline.AnnotationEvidence, Author: new("julian"), Body: "PagerDuty incident 4821 opened",
				CreatedAt: utc("2026-09-12T09:01:00Z"),
			},
			{
				ID: "a3", EventID: new("e2"), Kind: timeline.AnnotationEvidence, Author: new("julian"),
				Body: `{"t":{"$date":"2026-09-10T23:59:41.412+00:00"},"msg":"Slow query"}`, CreatedAt: utc("2026-09-12T09:30:00Z"),
			},
			{
				ID: "a4", EventID: new("e1"), Kind: timeline.AnnotationComment, Author: new("sam"),
				Body: "Lines up with the deploy.", CreatedAt: utc("2026-09-12T09:31:00Z"),
			},
			{
				ID: "a6", EventID: new("e3"), Kind: timeline.AnnotationComment, Body: "Election took three minutes.",
				CreatedAt: utc("2026-09-12T10:05:00Z"),
			},
		},
	)
	env.Attachments = []export.Attachment{
		attachmentOf(new("e3"), nil, "trace.pdf", "application/pdf", "b2", "2026-09-12T10:03:00Z"),
		attachmentOf(new("e3"), nil, "p99 latency.png", "image/png", "a1", "2026-09-12T10:02:00Z"),
		attachmentOf(nil, new("a5"), "rs-status.png", "image/png", "c3", "2026-09-12T10:00:30Z"),
		attachmentOf(nil, new("a2"), "incident.png", "image/png", "d4", "2026-09-12T09:01:30Z"),
		attachmentOf(nil, new("a4"), "deploys.csv", "text/csv", "e5", "2026-09-12T09:31:30Z"),
	}

	return env
}

// overlay is two timelines read as one chronology: their events interleave, two of them at
// one instant, the same source and a source holding a colon appear in both, each timeline
// has an unsourced event and an annotation on the whole timeline, and one event is
// annotated.
func overlay() export.Envelope {
	event := func(id, timelineID string, seq int64, at string, source *string, text string) export.Event {
		return export.Event{
			ID: id, TimelineID: timelineID, Seq: new(seq), OccurredAt: utc(at), Precision: timeline.PrecisionSecond,
			Source: source, Tags: []string{}, Text: text,
		}
	}
	stepDown := event("m1", "t1", 1, "2026-09-10T14:02:03Z", new("mongod"), "Primary stepped down")
	stepDown.Tags = []string{"db"}
	election := event("m3", "t1", 3, "2026-09-10T14:03:00Z", nil, "Replica set has no primary")
	election.EndedAt = new(utc("2026-09-10T14:05:10Z"))
	rollback := event("c3", "t2", 3, "2026-09-10T14:07:00Z", nil, "Rolled back deploy 812")
	rollback.Precision = timeline.PrecisionMinute
	rollback.Tags = []string{"deploy"}

	env := envelope(
		export.Timeline{ID: "t1", Name: "mongo-failover", Title: "MongoDB primary failover", Refs: []string{"INC-4820"}},
		[]export.Event{
			event("c1", "t2", 1, "2026-09-10T14:01:30Z", new("slack"), "maria: checkout p99 is 4s"),
			stepDown,
			event("c2", "t2", 2, "2026-09-10T14:02:40Z", new("k8s:events"), "checkout-api scaled to 12 pods"),
			event("m2", "t1", 2, "2026-09-10T14:02:40Z", new("k8s:events"), "db-2 pod restarted"),
			election,
			rollback,
		},
		[]export.Annotation{
			{
				ID: "a1", TimelineID: "t1", Kind: timeline.AnnotationComment, Author: new("sam"),
				Body: "Started from the replica set alert.", CreatedAt: utc("2026-09-12T09:00:00Z"),
			},
			{
				ID: "a2", TimelineID: "t2", Kind: timeline.AnnotationEvidence, Author: new("julian"),
				Body: "PagerDuty incident 4821 opened", CreatedAt: utc("2026-09-12T09:01:00Z"),
			},
			{
				ID: "a3", TimelineID: "t1", EventID: new("m1"), Kind: timeline.AnnotationEvidence,
				Body: "replSetStepDown in the mongod log", CreatedAt: utc("2026-09-12T09:30:00Z"),
			},
		},
	)
	env.Timelines = append(env.Timelines, export.Timeline{
		ID: "t2", Name: "checkout-latency", Title: "Checkout latency",
		Refs: []string{"INC-4821", "https://status.example.com/incidents/4821"},
	})

	return env
}

// attachmentOf returns an attachment of the event or annotation with the given id, whose
// SHA-256 is hex repeated.
func attachmentOf(eventID, annotationID *string, filename, mime, hex, created string) export.Attachment {
	return export.Attachment{
		ID: "f-" + filename, EventID: eventID, AnnotationID: annotationID, Filename: filename, MIME: mime,
		SHA256: strings.Repeat(hex, 32), Size: 2048, CreatedAt: utc(created),
	}
}

// precisions has one event at every precision, approximate times, intervals within a day
// and across midnight, a day-precision interval, an hour stated in +05:30 that starts
// half past an hour in UTC, and a day-precision event stated in -05:00 that belongs to the
// day it names although an hour-precision event on that day is earlier.
func precisions() export.Envelope {
	event := func(seq int64, at string, precision timeline.Precision, text string) export.Event {
		return export.Event{
			ID: "e" + strconv.FormatInt(seq, 10), Seq: new(seq), OccurredAt: utc(at), Precision: precision, Tags: []string{}, Text: text,
		}
	}
	dayInNewYork := event(1, "2026-09-11T05:00:00Z", timeline.PrecisionDay, "Deploy window opened")
	dayInNewYork.OccurredOffset = new(-5 * 60 * 60)
	dayInterval := event(12, "2026-09-11T00:00:00Z", timeline.PrecisionDay, "Error budget burned")
	dayInterval.EndedAt = new(utc("2026-09-12T00:00:00Z"))
	hourInKolkata := event(13, "2026-09-11T04:30:00Z", timeline.PrecisionHour, "Support in Bengaluru escalated")
	hourInKolkata.OccurredOffset = new(5*60*60 + 30*60)
	approximateMinute := event(4, "2026-09-11T03:14:00Z", timeline.PrecisionMinute, "Someone restarted db-2")
	approximateMinute.Approximate = true
	interval := event(9, "2026-09-11T03:15:00Z", timeline.PrecisionSecond, "Replica set has no primary")
	interval.EndedAt = new(utc("2026-09-11T03:16:30Z"))
	acrossMidnight := event(10, "2026-09-11T23:50:00Z", timeline.PrecisionMinute, "Nightly backup ran")
	acrossMidnight.EndedAt = new(utc("2026-09-12T00:20:00Z"))
	approximateDay := event(11, "2026-09-12T00:00:00Z", timeline.PrecisionDay, "Customer reports trickled in")
	approximateDay.Approximate = true

	return envelope(
		export.Timeline{ID: "t1", Name: "precisions", Refs: []string{}},
		[]export.Event{
			dayInNewYork,
			dayInterval,
			hourInKolkata,
			event(3, "2026-09-11T03:00:00Z", timeline.PrecisionHour, "Traffic started climbing"),
			approximateMinute,
			event(5, "2026-09-11T03:14:07Z", timeline.PrecisionSecond, "Primary stepped down"),
			event(6, "2026-09-11T03:14:07.180Z", timeline.PrecisionMillisecond, "Slow query logged"),
			event(7, "2026-09-11T03:14:07.180342Z", timeline.PrecisionMicrosecond, "Driver retried"),
			event(8, "2026-09-11T03:14:07.180342917Z", timeline.PrecisionNanosecond, "Kernel dropped a packet"),
			interval,
			acrossMidnight,
			approximateDay,
		},
		[]export.Annotation{},
	)
}

// acrossFallBack has events on the day New York leaves daylight saving, 01:30 happening
// twice, and on the ordinary day before it.
func acrossFallBack() export.Envelope {
	return envelope(
		export.Timeline{ID: "t1", Name: "fall-back", Title: "Clock change", Refs: []string{}},
		[]export.Event{
			{
				ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-10-31T16:00:00Z"), Precision: timeline.PrecisionSecond,
				Tags: []string{}, Text: "Deploy 812 started",
			},
			{
				ID: "e2", Seq: new(int64(2)), OccurredAt: utc("2026-11-01T05:30:00Z"), Precision: timeline.PrecisionSecond,
				Tags: []string{}, Text: "Primary stepped down",
			},
			{
				ID: "e3", Seq: new(int64(3)), OccurredAt: utc("2026-11-01T06:30:00Z"), Precision: timeline.PrecisionSecond,
				EndedAt: new(utc("2026-11-01T06:45:00Z")), Tags: []string{}, Text: "Replica set has no primary",
			},
		},
		[]export.Annotation{},
	)
}

func newYork() *time.Location {
	GinkgoHelper()
	loc, err := time.LoadLocation("America/New_York")
	Expect(err).NotTo(HaveOccurred())

	return loc
}

func parseMarkdown(source []byte) ast.Node {
	return goldmark.New().Parser().Parse(text.NewReader(source))
}

// parseGFM parses source as the apps an export is pasted into do: GitHub Flavored Markdown,
// with its strikethrough and autolinks.
func parseGFM(source []byte) ast.Node {
	return goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
}

// visibleText returns the text a reader sees in n: escapes and entities resolved, line
// breaks as \n, code and links as written, and nothing of nested lists.
func visibleText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.List:
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink:
			b.Write(n.Label(source))
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			value := n.Segment.Value(source)
			if n.IsRaw() {
				b.Write(value)
			} else {
				// The HTML writer resolves escapes and entities in one pass, as a renderer does.
				var rendered bytes.Buffer
				w := bufio.NewWriter(&rendered)
				html.DefaultWriter.Write(w, value)
				_ = w.Flush()
				b.WriteString(stdhtml.UnescapeString(rendered.String()))
			}
			if n.HardLineBreak() || n.SoftLineBreak() {
				b.WriteByte('\n')
			}
		case *ast.FencedCodeBlock:
			for i := range n.Lines().Len() {
				line := n.Lines().At(i)
				b.Write(line.Value(source))
			}
			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})

	return b.String()
}

// autoLinks returns the text of every autolink in n, in order.
func autoLinks(n ast.Node, source []byte) []string {
	var links []string
	_ = ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if link, ok := n.(*ast.AutoLink); ok && entering {
			links = append(links, string(link.Label(source)))
		}

		return ast.WalkContinue, nil
	})

	return links
}

// shownLink is an image or link as a reader's renderer resolves it.
type shownLink struct {
	Image             bool
	Text, Destination string
}

// shownLinks returns every image and link in n, in order, with escapes in their destinations
// resolved.
func shownLinks(n ast.Node, source []byte) []shownLink {
	var found []shownLink
	_ = ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image:
			found = append(found, shownLink{true, visibleText(n, source), string(util.UnescapePunctuations(n.Destination))})
		case *ast.Link:
			found = append(found, shownLink{false, visibleText(n, source), string(util.UnescapePunctuations(n.Destination))})
		}

		return ast.WalkContinue, nil
	})

	return found
}

// listDepth returns how many lists hold n, counting n when it is one.
func listDepth(n ast.Node) int {
	depth := 0
	for ; n != nil; n = n.Parent() {
		if n.Kind() == ast.KindList {
			depth++
		}
	}

	return depth
}

// headingPath is the text of the latest heading at each level, reset below a new heading.
type headingPath [6]string

// visit makes h, showing text, the latest heading at its level, and returns a key naming it
// by the headings above it and its own text.
func (p *headingPath) visit(h *ast.Heading, text string) string {
	p[h.Level-1] = text
	clear(p[h.Level:])

	return strings.Join(p[:h.Level], "\x00")
}

// listItems returns the items of the last list directly inside parent.
func listItems(parent ast.Node) []ast.Node {
	GinkgoHelper()
	var list ast.Node
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Kind() == ast.KindList {
			list = c
		}
	}
	Expect(list).NotTo(BeNil(), "no list")

	var items []ast.Node
	for c := list.FirstChild(); c != nil; c = c.NextSibling() {
		items = append(items, c)
	}

	return items
}

var _ = DescribeTable("ParseGroupBy parses the groupings it knows, and nothing else",
	func(name string, want export.GroupBy, known bool) {
		got, ok := export.ParseGroupBy(name)
		Expect(ok).To(Equal(known))
		Expect(got).To(Equal(want))
	},
	Entry("day, by default", "", export.GroupByDay, true),
	Entry("source", "source", export.GroupBySource, true),
	Entry("tag", "tag", export.GroupByTag, true),
	Entry("an unknown name", "kind", export.GroupByDay, false),
	Entry("a name in another case", "Source", export.GroupByDay, false),
)

var _ = Describe("WriteMarkdown", func() {
	It("outlines a timeline: title, metadata, timeline annotations, then events by day with their annotations", func() {
		matchGolden("chronological", markdown(checkoutSpike(), export.MarkdownOptions{Zone: time.UTC}))
	})

	It("shows each time at its precision, approximate times and intervals, with day-precision events first in the day they name", func() {
		matchGolden("precisions", markdown(precisions(), export.MarkdownOptions{Zone: time.UTC}))
	})

	It("labels each day with the zone's abbreviations and gives every time an offset on a day the offset changes", func() {
		matchGolden("dst-new-york", markdown(acrossFallBack(), export.MarkdownOptions{Zone: newYork()}))
	})

	It("sections events by source in order of first appearance, with days inside and unsourced events last", func() {
		matchGolden("group-by-source",
			markdown(checkoutSpike(), export.MarkdownOptions{Zone: time.UTC, GroupBy: export.GroupBySource}))
	})

	It("sections events under each of their tags, naming their other tags, with untagged events last", func() {
		matchGolden("group-by-tag", markdown(checkoutSpike(), export.MarkdownOptions{Zone: time.UTC, GroupBy: export.GroupByTag}))
	})

	It("adds each event's time as written under IncludeRaw", func() {
		env := acrossFallBack()
		env.Events[1].OccurredRaw = new("2026-11-01T01:30:00-04:00")

		matchGolden("include-raw", markdown(env, export.MarkdownOptions{Zone: time.UTC, IncludeRaw: true}))
	})

	It("overlays several timelines: each listed in the metadata, and every event and whole-timeline annotation labelled with its timeline", func() {
		matchGolden("overlay", markdown(overlay(), export.MarkdownOptions{Zone: time.UTC}))
	})

	It("sections an overlay by source, keeping each event's timeline label", func() {
		matchGolden("overlay-group-by-source", markdown(overlay(), export.MarkdownOptions{Zone: time.UTC, GroupBy: export.GroupBySource}))
	})

	DescribeTable("fails, writing nothing, when an overlay holds a record of a timeline it does not export",
		func(orphan func(*export.Envelope)) {
			env := overlay()
			orphan(&env)
			var b bytes.Buffer

			Expect(export.WriteMarkdown(&b, env, export.MarkdownOptions{Zone: time.UTC})).To(MatchError(ContainSubstring("gone")))
			Expect(b.String()).To(BeEmpty())
		},
		Entry("an event", func(env *export.Envelope) { env.Events[0].TimelineID = "gone" }),
		Entry("an annotation on a whole timeline", func(env *export.Envelope) { env.Annotations[0].TimelineID = "gone" }),
	)

	It("appends the timeline's Mermaid diagram under Diagram", func() {
		matchGolden("diagram", markdown(checkoutSpike(), export.MarkdownOptions{Zone: time.UTC, Diagram: true}))
	})

	DescribeTable("ends the outline with a mermaid code block holding exactly the Mermaid export, in the same zone",
		func(env export.Envelope) {
			zone := newYork()
			withoutDiagram := markdown(env, export.MarkdownOptions{Zone: zone})
			source := []byte(markdown(env, export.MarkdownOptions{Zone: zone, Diagram: true}))

			Expect(string(source)).To(HavePrefix(withoutDiagram))
			block, ok := parseGFM(source).LastChild().(*ast.FencedCodeBlock)
			Expect(ok).To(BeTrue(), string(source))
			Expect(string(block.Language(source))).To(Equal("mermaid"))
			Expect(visibleText(block, source)).To(Equal(mermaid(env, zone)))
		},
		Entry("a timeline with sources, intervals and annotations", checkoutSpike()),
		Entry("hostile labels, backtick runs included", hostileLabels()),
		Entry("an overlay of two timelines", overlay()),
	)

	It("outlines a timeline with no title, refs, events or annotations as its name and counts", func() {
		matchGolden("empty", markdown(envelope(export.Timeline{ID: "t1", Name: "quiet", Refs: []string{}}, nil, nil),
			export.MarkdownOptions{Zone: time.UTC}))
	})

	DescribeTable("keeps hostile text visibly intact in event text, comments and evidence",
		func(input string) {
			env := envelope(
				export.Timeline{ID: "t1", Name: "hostile", Refs: []string{}},
				[]export.Event{{
					ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-09-11T03:14:07Z"), Precision: timeline.PrecisionSecond,
					Tags: []string{}, Text: input,
				}},
				[]export.Annotation{
					{
						ID: "a1", EventID: new("e1"), Kind: timeline.AnnotationComment, Author: new("julian"), Body: input,
						CreatedAt: utc("2026-09-12T09:00:00Z"),
					},
					{
						ID: "a2", EventID: new("e1"), Kind: timeline.AnnotationEvidence, Author: new("julian"), Body: input,
						CreatedAt: utc("2026-09-12T09:01:00Z"),
					},
				},
			)
			source := []byte(markdown(env, export.MarkdownOptions{Zone: time.UTC}))
			doc := parseGFM(source)

			event := listItems(doc)[0]
			Expect(visibleText(event, source)).To(Equal("03:14:07 · "+input), string(source))
			annotations := listItems(event)
			Expect(annotations).To(HaveLen(2), string(source))
			Expect(visibleText(annotations[0], source)).To(Equal("comment (julian): "+input), string(source))
			Expect(strings.TrimPrefix(visibleText(annotations[1], source), "evidence (julian):")).
				To(Or(Equal(" "+input), Equal(input+"\n")), string(source))
		},
		Entry("backticks", "run `rs.status()` then ``` and ``"),
		Entry("backticks at both ends", "`quoted`"),
		Entry("emphasis markers", "*bold*, _under_ and **strong**"),
		Entry("an HTML tag", "<script>alert(1)</script>"),
		Entry("an entity and a pipe", "&lt; is not < and a | b"),
		Entry("link syntax", "[x](https://example.com) and ![img](y)"),
		Entry("tildes", "~~struck~~ and ~one~"),
		Entry("a URL", "see https://grafana.example.com/d/x?org_id=1&from=now-1h here"),
		Entry("block markers at the start of lines", "intro\n#tag at line start\n- item\n+ item\n1. item\n2) item\n> quote\n===\n---\n~~~"),
		Entry("an empty line", "first\n\nthird"),
		Entry("a 10 KB line", strings.Repeat("ab*c", 2560)),
	)

	DescribeTable("keeps hostile text visibly intact in the title, refs and authors",
		func(input string) {
			env := envelope(
				export.Timeline{ID: "t1", Name: "hostile", Title: input, Refs: []string{input}},
				[]export.Event{},
				[]export.Annotation{{
					ID: "a1", Kind: timeline.AnnotationComment, Author: new(input), Body: "body",
					CreatedAt: utc("2026-09-12T09:00:00Z"),
				}},
			)
			source := []byte(markdown(env, export.MarkdownOptions{Zone: time.UTC}))
			doc := parseGFM(source)

			title, metadata := doc.FirstChild(), doc.FirstChild().NextSibling()
			Expect(visibleText(title, source)).To(Equal(input+" (hostile)"), string(source))
			Expect(visibleText(metadata.FirstChild(), source)).To(Equal("Refs: "+input), string(source))
			Expect(visibleText(listItems(doc)[0], source)).To(HavePrefix("comment ("+input+"):"), string(source))
		},
		Entry("backticks", "run `rs.status()` then ``` and ``"),
		Entry("emphasis markers", "*bold*, _under_ and **strong**"),
		Entry("an HTML tag", "<script>alert(1)</script>"),
		Entry("an entity and a pipe", "&lt; is not < and a | b"),
		Entry("link syntax", "[x](https://example.com) and ![img](y)"),
		Entry("tildes", "~~struck~~ and ~one~"),
		Entry("a URL", "see https://grafana.example.com/d/x?org_id=1&from=now-1h here"),
	)

	It("drops trailing blank lines from text, so no line break ends it", func() {
		env := envelope(
			export.Timeline{ID: "t1", Name: "trailing", Refs: []string{}},
			[]export.Event{{
				ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-09-11T03:14:07Z"), Precision: timeline.PrecisionSecond,
				Tags: []string{}, Text: "line one\n   \n\t",
			}},
			[]export.Annotation{},
		)
		source := []byte(markdown(env, export.MarkdownOptions{Zone: time.UTC}))

		Expect(visibleText(listItems(parseGFM(source))[0], source)).To(Equal("03:14:07 · line one"), string(source))
	})

	It("writes URLs as they are, so GFM autolinks each one whole", func() {
		const url = "https://grafana.example.com/d/x?org_id=1&from=now-1h"
		env := envelope(
			export.Timeline{ID: "t1", Name: "links", Title: "see " + url, Refs: []string{url}},
			[]export.Event{{
				ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-09-11T03:14:07Z"), Precision: timeline.PrecisionSecond,
				Tags: []string{}, Text: "see " + url + " here",
			}},
			[]export.Annotation{{
				ID: "a1", EventID: new("e1"), Kind: timeline.AnnotationComment, Author: new(url), Body: url + "\nand " + url,
				CreatedAt: utc("2026-09-12T09:00:00Z"),
			}},
		)
		source := []byte(markdown(env, export.MarkdownOptions{Zone: time.UTC}))

		Expect(autoLinks(parseGFM(source), source)).To(Equal(slices.Repeat([]string{url}, 6)), string(source))
	})

	It("shows an image attachment as an image and any other as a link, each to the file its hash names", func() {
		env := checkoutSpike()
		source := []byte(markdown(env, export.MarkdownOptions{Zone: time.UTC}))

		file := func(filename string) string {
			i := slices.IndexFunc(env.Attachments, func(a export.Attachment) bool { return a.Filename == filename })
			return "attachments/" + env.Attachments[i].SHA256 + filepath.Ext(filename)
		}
		Expect(shownLinks(parseGFM(source), source)).To(ConsistOf(
			shownLink{true, "incident.png", file("incident.png")},
			shownLink{false, "deploys.csv", file("deploys.csv")},
			shownLink{true, "p99 latency.png", file("p99 latency.png")},
			shownLink{false, "trace.pdf", file("trace.pdf")},
			shownLink{true, "rs-status.png", file("rs-status.png")},
		), string(source))
	})

	DescribeTable("keeps a hostile filename to the text of its image or link, whose destination it never reaches",
		func(filename, shown string) {
			const hash = "3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a3f9a"
			env := envelope(
				export.Timeline{ID: "t1", Name: "hostile", Refs: []string{}},
				[]export.Event{{
					ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-09-11T03:14:07Z"), Precision: timeline.PrecisionSecond,
					Tags: []string{}, Text: "Primary stepped down",
				}},
				[]export.Annotation{{
					ID: "a1", EventID: new("e1"), Kind: timeline.AnnotationComment, Body: "see attached", CreatedAt: utc("2026-09-12T09:00:00Z"),
				}},
			)
			env.Attachments = []export.Attachment{
				{ID: "f1", EventID: new("e1"), Filename: filename, MIME: "image/png", SHA256: hash, CreatedAt: utc("2026-09-12T09:00:00Z")},
				{ID: "f2", AnnotationID: new("a1"), Filename: filename, MIME: "application/pdf", SHA256: hash, CreatedAt: utc("2026-09-12T09:00:00Z")},
			}
			source := []byte(markdown(env, export.MarkdownOptions{Zone: time.UTC}))

			Expect(shownLinks(parseGFM(source), source)).To(HaveExactElements(
				shownLink{true, shown, "attachments/" + hash + ".png"},
				shownLink{false, shown, "attachments/" + hash + ".pdf"},
			), string(source))
		},
		Entry("a link that tries to close the text early", ") [x](evil", ") [x](evil"),
		Entry("brackets", "a]b[c].png", "a]b[c].png"),
		Entry("a URL holding a bracket", "https://x.test/a](evil", "https://x.test/a](evil"),
		Entry("backticks", "`rs.status()`.png", "`rs.status()`.png"),
		Entry("emphasis markers and a pipe", "*bold*_under_|pipe.png", "*bold*_under_|pipe.png"),
		Entry("an HTML tag", "<img src=x onerror=alert(1)>.png", "<img src=x onerror=alert(1)>.png"),
		Entry("a backslash before a bracket", `back\]slash.png`, `back\]slash.png`),
		Entry("a line break, shown as a space", "first\nsecond.png", "first second.png"),
	)

	It("links each attachment where AttachmentLink says, even a path with spaces, parentheses and angle brackets", func() {
		const path = `/Users/sam/Library/Application Support/coroner (old)/<blobs>/3f9a`
		env := checkoutSpike()
		env.Attachments = env.Attachments[:1]
		opts := export.MarkdownOptions{Zone: time.UTC, AttachmentLink: func(export.Attachment) string { return path }}
		source := []byte(markdown(env, opts))

		Expect(shownLinks(parseGFM(source), source)).To(HaveExactElements(shownLink{false, "trace.pdf", path}), string(source))
	})

	DescribeTable("fails, writing nothing, when an attachment's owner is not in the export",
		func(eventID, annotationID *string) {
			env := checkoutSpike()
			env.Attachments = append(env.Attachments, attachmentOf(eventID, annotationID, "orphan.png", "image/png", "ff", "2026-09-12T09:00:00Z"))
			var b bytes.Buffer

			Expect(export.WriteMarkdown(&b, env, export.MarkdownOptions{Zone: time.UTC})).To(MatchError(ContainSubstring("orphan.png")))
			Expect(b.String()).To(BeEmpty())
		},
		Entry("an event", new("gone"), nil),
		Entry("an annotation", nil, new("gone")),
	)

	It("fails, writing nothing, when an annotation's event is not in the export", func() {
		env := checkoutSpike()
		env.Events = env.Events[:1]
		var b bytes.Buffer

		Expect(export.WriteMarkdown(&b, env, export.MarkdownOptions{Zone: time.UTC})).To(MatchError(ContainSubstring("e2")))
		Expect(b.String()).To(BeEmpty())
	})

	It("fails, writing nothing, for a grouping it does not know", func() {
		var b bytes.Buffer

		Expect(export.WriteMarkdown(&b, checkoutSpike(), export.MarkdownOptions{GroupBy: "kind"})).
			To(MatchError(ContainSubstring(`"kind"`)))
		Expect(b.String()).To(BeEmpty())
	})

	It("fails, writing nothing, when the export holds no timeline", func() {
		env := checkoutSpike()
		env.Timelines = []export.Timeline{}
		var b bytes.Buffer

		Expect(export.WriteMarkdown(&b, env, export.MarkdownOptions{Zone: time.UTC})).NotTo(Succeed())
		Expect(b.String()).To(BeEmpty())
	})
})

var _ = Describe("Markdown goldens", func() {
	goldens, err := filepath.Glob(filepath.Join(goldenDir, "*.md"))
	if err != nil {
		panic(err)
	}

	for _, path := range goldens {
		It("keeps "+path+" to plain CommonMark: no raw HTML, lists at most two deep, one heading per day or group", func() {
			source, err := os.ReadFile(filepath.Clean(path))
			Expect(err).NotTo(HaveOccurred())
			doc := parseMarkdown(source)

			titles, seen := 0, map[string]bool{}
			var headings headingPath
			Expect(ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if !entering {
					return ast.WalkContinue, nil
				}
				Expect(n.Kind()).NotTo(BeElementOf(ast.KindHTMLBlock, ast.KindRawHTML), "raw HTML")
				if n.Kind() == ast.KindList {
					Expect(listDepth(n)).To(BeNumerically("<=", 2), "list depth")
				}
				if heading, ok := n.(*ast.Heading); ok {
					if heading.Level == 1 {
						titles++
					}
					key := headings.visit(heading, visibleText(heading, source))
					Expect(seen).NotTo(HaveKey(key), "heading repeated under the same parent: "+visibleText(heading, source))
					seen[key] = true
				}

				return ast.WalkContinue, nil
			})).To(Succeed())
			Expect(titles).To(Equal(1), "level-1 headings")
		})
	}

	It("passes rumdl check, line length aside", func(ctx SpecContext) {
		_, err := exec.LookPath("rumdl")
		Expect(err).NotTo(HaveOccurred(), "rumdl is not on PATH: run the suite through mise run test")

		out, err := exec.CommandContext(ctx, "rumdl", "check", "--no-exclude", "--disable", "MD013", goldenDir).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))
	})
})
