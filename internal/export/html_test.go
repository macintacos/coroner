package export_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

const htmlGoldenDir = "testdata/html"

// hostile opens a script element, closes one, injects markup and an HTML comment, and holds
// an entity and a bare ampersand.
const hostile = `</script><img src=x onerror=alert(1)> &amp; & <!-- <b>`

func page(env export.Envelope, zone *time.Location) string {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(export.WriteHTML(&b, env, zone)).To(Succeed())

	return b.String()
}

// elideAssets returns page with the bytes of each file the page inlines replaced by a comment
// naming it, and fails unless each is inlined exactly once.
func elideAssets(page string) string {
	GinkgoHelper()
	for _, name := range []string{"html/vendor/vis-timeline-graph2d.min.js", "html/viewer.js", "html/viewer.css"} {
		asset, err := os.ReadFile(filepath.Clean(name))
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Count(page, string(asset))).To(Equal(1), name)
		page = strings.Replace(page, string(asset), "/* "+name+" */", 1)
	}

	return page
}

// jsonBlock returns the content of the application/json script element with id.
func jsonBlock(page, id string) []byte {
	GinkgoHelper()
	match := regexp.MustCompile(`(?s)<script type="application/json" id="` + id + `">(.*?)</script>`).FindStringSubmatch(page)
	Expect(match).To(HaveLen(2), "no "+id+" block")

	return []byte(match[1])
}

type zoneSpan struct {
	Start        int64  `json:"start"`
	Offset       int    `json:"offset"`
	Abbreviation string `json:"abbreviation"`
}

type pageZone struct {
	Name  string     `json:"name"`
	Spans []zoneSpan `json:"spans"`
}

func zoneOf(page string) pageZone {
	GinkgoHelper()
	var z pageZone
	Expect(json.Unmarshal(jsonBlock(page, "coroner-zone"), &z)).To(Succeed())

	return z
}

func unixMilli(value string) int64 {
	return time.Time(utc(value)).UnixMilli()
}

// hostileExport carries hostile text in every string a person typed.
func hostileExport() export.Envelope {
	return envelope(
		export.Timeline{ID: "t1", Name: "hostile", Title: hostile, Refs: []string{hostile}},
		[]export.Event{
			{
				ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-09-11T03:14:07Z"), Precision: timeline.PrecisionSecond,
				Source: new(hostile), Tags: []string{"</script>"}, Text: hostile, URL: new("javascript:alert(1)"), Author: new(hostile),
			},
		},
		[]export.Annotation{
			{
				ID: "a1", EventID: new("e1"), Kind: timeline.AnnotationEvidence, Author: new(hostile), Body: hostile,
				CreatedAt: utc("2026-09-12T09:00:00Z"),
			},
		},
	)
}

var _ = Describe("WriteHTML", func() {
	It("lays out one page: the viewer's containers, then the export and zone as data, the library and the viewer", func() {
		matchGoldenFile(htmlGoldenDir, "checkout-spike", ".html", elideAssets(page(checkoutSpike(), time.UTC)))
	})

	It("keeps hostile text out of the markup, embedding it only as JSON escaped for HTML", func() {
		matchGoldenFile(htmlGoldenDir, "hostile", ".html", elideAssets(page(hostileExport(), time.UTC)))
	})

	It("keeps every script element closing where it should, with the export intact in the data block", func() {
		p := page(hostileExport(), time.UTC)

		Expect(strings.Count(p, "<script")).To(Equal(4))
		Expect(strings.Count(p, "</script>")).To(Equal(4))
		Expect(p).NotTo(ContainSubstring("<img"))
		Expect(p).NotTo(ContainSubstring("<!--"))
		var got export.Envelope
		Expect(json.Unmarshal(jsonBlock(p, "coroner-data"), &got)).To(Succeed())
		Expect(got.Timelines[0].Title).To(Equal(hostile))
		Expect(*got.Events[0].Source).To(Equal(hostile))
		Expect(got.Events[0].Text).To(Equal(hostile))
		Expect(got.Annotations[0].Body).To(Equal(hostile))
	})

	DescribeTable("links and loads nothing from the network",
		func(env export.Envelope) {
			p := page(env, time.UTC)

			Expect(p).NotTo(MatchRegexp(`(?i)\b(src|href)\s*=\s*["']?\s*(https?:)?//`))
			Expect(p).NotTo(MatchRegexp(`(?i)<(link|img|iframe|object|embed)\b|<script[^>]*\bsrc\b`))
		},
		Entry("with URLs in refs and events", checkoutSpike()),
		Entry("with hostile text", hostileExport()),
	)

	It("gives the viewer the zone's offset from a year before the export's first instant to a year after its last", func() {
		z := zoneOf(page(acrossFallBack(), newYork()))

		Expect(z.Name).To(Equal("America/New_York"))
		Expect(z.Spans).To(Equal([]zoneSpan{
			{Start: unixMilli("2025-09-12T21:40:05Z"), Offset: -4 * 60 * 60, Abbreviation: "EDT"},
			{Start: unixMilli("2025-11-02T06:00:00Z"), Offset: -5 * 60 * 60, Abbreviation: "EST"},
			{Start: unixMilli("2026-03-08T07:00:00Z"), Offset: -4 * 60 * 60, Abbreviation: "EDT"},
			{Start: unixMilli("2026-11-01T06:00:00Z"), Offset: -5 * 60 * 60, Abbreviation: "EST"},
			{Start: unixMilli("2027-03-14T07:00:00Z"), Offset: -4 * 60 * 60, Abbreviation: "EDT"},
		}))
	})

	It("gives the viewer one span for a zone without transitions, anchored on the export time when there are no events", func() {
		z := zoneOf(page(envelope(export.Timeline{ID: "t1", Name: "quiet", Refs: []string{}}, nil, nil), nil))

		Expect(z).To(Equal(pageZone{
			Name:  "UTC",
			Spans: []zoneSpan{{Start: unixMilli("2025-09-12T21:40:05Z"), Offset: 0, Abbreviation: "UTC"}},
		}))
	})

	DescribeTable("fails, writing nothing, unless the export holds one timeline",
		func(timelines []export.Timeline) {
			env := checkoutSpike()
			env.Timelines = timelines
			var b bytes.Buffer

			Expect(export.WriteHTML(&b, env, time.UTC)).To(MatchError(ContainSubstring("want one timeline")))
			Expect(b.Len()).To(BeZero())
		},
		Entry("none", []export.Timeline{}),
		Entry("two", []export.Timeline{{ID: "t1", Name: "a"}, {ID: "t2", Name: "b"}}),
	)
})
