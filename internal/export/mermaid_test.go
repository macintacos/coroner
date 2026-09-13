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
// parser would otherwise read as syntax: colons, entities, comments, directives, keywords
// and dates at the start of a label, line breaks, and a 10 KB line.
func hostileLabels() export.Envelope {
	texts := []string{
		"deploy-bot: checkout-api v2.31.0 rolled out",
		"literal #58; and #35; stay literal",
		"run `rs.status()` then ``` and ``",
		"%% not a comment",
		"5% of requests failed",
		`%%{init: {"theme":"forest"}}%% stays`,
		`semi; colon "double" 'single' <b>bold</b> & <script>alert(1)</script>`,
		"first line\nsecond line\r\nthird line",
		"section 5 failed",
		"Title of the runbook",
		"click here for logs",
		"gantt chart attached",
		"2026-09-10 deploy",
		"😀 on-call paged",
		strings.Repeat("ab:c#", 2000),
	}
	start := time.Date(2026, time.September, 11, 3, 14, 0, 0, time.UTC)
	events := make([]export.Event, 0, len(texts))
	for i, text := range texts {
		e := export.Event{
			ID: "e" + string(rune('a'+i)), Seq: new(int64(i + 1)), OccurredAt: export.Instant(start.Add(time.Duration(i) * time.Second)),
			Precision: timeline.PrecisionSecond, Tags: []string{}, Text: text,
		}
		if i < 7 {
			e.Source = new("db-2:mongod")
		}
		events = append(events, e)
	}

	return envelope(
		export.Timeline{ID: "t1", Name: "hostile", Title: "Postmortem: #4821 at 100%", Refs: []string{}},
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

	It("shows times on the zone's wall clock and names the zone in the title", func() {
		diagram := mermaid(acrossFallBack(), newYork())

		Expect(diagram).To(ContainSubstring("  title fall-back Clock change (America/New_York)\n"))
		Expect(strings.Count(diagram, "2026-11-01T01:30:00")).To(Equal(2), diagram)
	})

	It("writes the same bytes for the same export", func() {
		Expect(mermaid(hostileLabels(), time.UTC)).To(Equal(mermaid(hostileLabels(), time.UTC)))
	})

	DescribeTable("fails, writing nothing, unless the export holds one timeline",
		func(timelines []export.Timeline) {
			env := checkoutSpike()
			env.Timelines = timelines
			var b bytes.Buffer

			Expect(export.WriteMermaid(&b, env, time.UTC)).NotTo(Succeed())
			Expect(b.String()).To(BeEmpty())
		},
		Entry("none", []export.Timeline{}),
		Entry("two", []export.Timeline{{Name: "a"}, {Name: "b"}}),
	)
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
		env.Annotations = nil
		env.Events[0].Text = strings.Repeat("x", export.MermaidMaxTextSize)

		size, err := export.MermaidSize(env, time.UTC)

		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(BeNumerically(">", export.MermaidMaxTextSize))
	})
})
