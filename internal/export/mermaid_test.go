package export_test

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf16"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

const mermaidGoldenDir = "testdata/mermaid"

func mermaid(env export.Envelope, zone *time.Location) string {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(export.WriteMermaid(&b, env, zone)).To(Succeed())

	return b.String()
}

// hostileLabels is a timeline whose title, source and event text hold what Mermaid's gantt
// parser would otherwise read as syntax: colons, semicolons, entities, comments, directives,
// keywords and dates at the start of a label, keywords around JavaScript-only whitespace, line
// breaks, and a 10 KB line.
func hostileLabels() export.Envelope {
	source := new("db-2:mongod;primary")
	events := []export.Event{
		{Source: source, Text: "deploy-bot: checkout-api v2.31.0 rolled out"},
		{Source: source, Text: "literal #58; and #35; stay literal"},
		{Source: source, Text: "run `rs.status()` then ``` and ``"},
		{Source: source, Text: "%% not a comment"},
		{Source: source, Text: "5% of requests failed"},
		{Source: source, Text: `%%{init: {"theme":"forest"}}%% stays`},
		{Source: source, Text: `semi; colon "double" 'single' <b>bold</b> & <script>alert(1)</script>`},
		{Text: "first line\nsecond line\r\nthird line"},
		{Text: "section 5 failed"},
		{Text: "Title of the runbook"},
		{Text: "click here for logs"},
		{Text: "gantt chart attached"},
		{Text: "2026-09-10 deploy"},
		{Text: "😀 on-call paged"},
		{Text: "section\u00a05 failed with an NBSP"},
		{Text: "\ufeffclick with a leading BOM"},
		{Text: "click to expand", Approximate: true},
		{Text: strings.Repeat("ab:c#", 2000)},
	}
	start := time.Date(2026, time.September, 11, 3, 14, 0, 0, time.UTC)
	for i := range events {
		events[i].ID = "e" + string(rune('a'+i))
		events[i].Seq = new(int64(i + 1))
		events[i].OccurredAt = export.Instant(start.Add(time.Duration(i) * time.Second))
		events[i].Precision = timeline.PrecisionSecond
		events[i].Tags = []string{}
	}

	return envelope(
		export.Timeline{ID: "t1", Name: "hostile", Title: "Postmortem: #4821 at 100%; take 2", Refs: []string{}},
		events,
		[]export.Annotation{},
	)
}

var _ = Describe("WriteMermaid", func() {
	It("draws a gantt: a section per source in order of first appearance, bars for intervals, milestones for instants, ~ for approximate times", func() {
		matchGoldenFile(mermaidGoldenDir, "checkout-spike", ".mmd", mermaid(checkoutSpike(), time.UTC))
	})

	It("places every precision at its second, day-precision events included", func() {
		matchGoldenFile(mermaidGoldenDir, "precisions", ".mmd", mermaid(precisions(), time.UTC))
	})

	It("keeps hostile text in the title, section names and labels from reading as gantt syntax", func() {
		matchGoldenFile(mermaidGoldenDir, "hostile", ".mmd", mermaid(hostileLabels(), time.UTC))
	})

	It("draws a timeline with no events as its title alone", func() {
		matchGoldenFile(mermaidGoldenDir, "empty", ".mmd", mermaid(envelope(export.Timeline{ID: "t1", Name: "quiet", Refs: []string{}}, nil, nil), time.UTC))
	})

	It("shows times on the zone's wall clock and names the zone in the title", func() {
		env := acrossFallBack()
		env.Events = append(env.Events, export.Event{
			ID: "e4", TimelineID: env.Timelines[0].ID, Seq: new(int64(4)), OccurredAt: utc("2026-11-01T05:50:00Z"), Precision: timeline.PrecisionSecond,
			EndedAt: new(utc("2026-11-01T06:10:00Z")), Tags: []string{}, Text: "Failover window",
		})

		diagram := mermaid(env, newYork())

		Expect(diagram).To(ContainSubstring("  title fall-back Clock change (EDT/EST)\n"))
		Expect(strings.Count(diagram, "2026-11-01T01:30:00")).To(Equal(2), diagram)
		Expect(diagram).To(ContainSubstring("    Failover window :2026-11-01T01:50:00, 1200s\n"))
	})

	It("names the zone at export in the title of a timeline of day-precision events only", func() {
		env := envelope(export.Timeline{ID: "t1", Name: "winter", Title: "Outage", Refs: []string{}}, []export.Event{{
			ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-01-15T05:00:00Z"), OccurredOffset: new(-18000),
			Precision: timeline.PrecisionDay, Tags: []string{}, Text: "Snow day",
		}}, nil)

		Expect(mermaid(env, newYork())).To(ContainSubstring("  title winter Outage (EDT)\n"))
	})

	It("names the abbreviation its bars are drawn in when an event reads in its own offset", func() {
		env := envelope(export.Timeline{ID: "t1", Name: "winter", Title: "Outage", Refs: []string{}}, []export.Event{{
			ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-01-15T04:30:00Z"), OccurredOffset: new(19800),
			Precision: timeline.PrecisionHour, Tags: []string{}, Text: "Paged",
		}}, nil)

		Expect(mermaid(env, newYork())).To(ContainSubstring("  title winter Outage (EST)\n"))
	})

	It("overlays several timelines: a section per timeline and source in order of first appearance, the title naming each timeline", func() {
		matchGoldenFile(mermaidGoldenDir, "overlay", ".mmd", mermaid(overlay(), time.UTC))
	})
})

var _ = Describe("MermaidSize", func() {
	It("measures the diagram as Mermaid does, in UTF-16 code units", func() {
		size, err := export.MermaidSize(hostileLabels(), time.UTC)

		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(Equal(len(utf16.Encode([]rune(mermaid(hostileLabels(), time.UTC))))))
		Expect(size).To(BeNumerically("<", len(mermaid(hostileLabels(), time.UTC))), "an emoji is 4 bytes but 2 code units")
	})

	It("exceeds MermaidMaxTextSize for an export Mermaid refuses to draw", func() {
		env := checkoutSpike()
		env.Events[0].Text = strings.Repeat("x", export.MermaidMaxTextSize)

		size, err := export.MermaidSize(env, time.UTC)

		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(BeNumerically(">", export.MermaidMaxTextSize))
	})
})
