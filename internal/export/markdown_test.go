package export_test

import (
	"bufio"
	"bytes"
	stdhtml "html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"

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
	env.Events = events
	env.Annotations = annotations

	return env
}

// checkoutSpike is a timeline over two days: sourced and unsourced events, tags, an
// interval, and annotations on the timeline and on events, one of them multi-line.
func checkoutSpike() export.Envelope {
	return envelope(
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
}

// precisions has one event at every precision, approximate times, intervals within a day
// and across midnight, and a day-precision event stated in -05:00 that belongs to the day
// it names although an hour-precision event on that day is earlier.
func precisions() export.Envelope {
	event := func(seq int64, at string, precision timeline.Precision, text string) export.Event {
		return export.Event{
			ID: "e" + strconv.FormatInt(seq, 10), Seq: new(seq), OccurredAt: utc(at), Precision: precision, Tags: []string{}, Text: text,
		}
	}
	dayInNewYork := event(1, "2026-09-11T05:00:00Z", timeline.PrecisionDay, "Deploy window opened")
	dayInNewYork.OccurredOffset = new(-5 * 60 * 60)
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

// visibleText returns the text a reader sees in n: escapes and entities resolved, line
// breaks as \n, code as written, and nothing of nested lists.
func visibleText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.List:
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
			doc := parseMarkdown(source)

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
		Entry("block markers at the start of lines", "intro\n#tag at line start\n- item\n+ item\n1. item\n2) item\n> quote\n===\n---\n~~~"),
		Entry("an empty line", "first\n\nthird"),
		Entry("a 10 KB line", strings.Repeat("ab*c", 2560)),
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

	DescribeTable("fails, writing nothing, unless the export holds one timeline",
		func(timelines []export.Timeline) {
			env := checkoutSpike()
			env.Timelines = timelines
			var b bytes.Buffer

			Expect(export.WriteMarkdown(&b, env, export.MarkdownOptions{Zone: time.UTC})).NotTo(Succeed())
			Expect(b.String()).To(BeEmpty())
		},
		Entry("none", []export.Timeline{}),
		Entry("two", []export.Timeline{{Name: "a"}, {Name: "b"}}),
	)
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
			parents := map[int]string{}
			Expect(ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if !entering {
					return ast.WalkContinue, nil
				}
				Expect(n.Kind()).NotTo(BeElementOf(ast.KindHTMLBlock, ast.KindRawHTML), "raw HTML")
				if n.Kind() == ast.KindList {
					depth := 0
					for p := n; p != nil; p = p.Parent() {
						if p.Kind() == ast.KindList {
							depth++
						}
					}
					Expect(depth).To(BeNumerically("<=", 2), "list depth")
				}
				if heading, ok := n.(*ast.Heading); ok {
					if heading.Level == 1 {
						titles++
					}
					path := make([]string, 0, heading.Level)
					for level := 1; level < heading.Level; level++ {
						path = append(path, parents[level])
					}
					key := strings.Join(append(path, visibleText(heading, source)), "\x00")
					Expect(seen).NotTo(HaveKey(key), "heading repeated under the same parent: "+visibleText(heading, source))
					seen[key] = true
					parents[heading.Level] = visibleText(heading, source)
					for level := heading.Level + 1; level <= 6; level++ {
						delete(parents, level)
					}
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
