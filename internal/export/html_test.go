package export_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rogpeppe/go-internal/txtar"

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
	for _, path := range []string{"html/vendor/vis-timeline-graph2d.min.js", "html/viewer.js", "html/viewer.css"} {
		asset, err := os.ReadFile(filepath.Clean(path))
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Count(page, string(asset))).To(Equal(1), path)
		page = strings.Replace(page, string(asset), "/* "+path+" */", 1)
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

// archiveFile returns the content of the file named name in the txtar archive at path.
func archiveFile(path, name string) []byte {
	GinkgoHelper()
	archive, err := txtar.ParseFile(path)
	Expect(err).NotTo(HaveOccurred())
	i := slices.IndexFunc(archive.Files, func(f txtar.File) bool { return f.Name == name })
	Expect(i).NotTo(Equal(-1), "no "+name+" in "+path)

	return archive.Files[i].Data
}

// hostileExport carries hostile text in the typed strings the viewer shows.
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
				URL: new(hostile), Source: new(hostile), CreatedAt: utc("2026-09-12T09:00:00Z"),
			},
		},
	)
}

// hostileOverlay is overlay with hostile text in its second timeline's title, the source both
// timelines share, an event's text and an annotation's body.
func hostileOverlay() export.Envelope {
	env := overlay()
	env.Timelines[1].Title = hostile
	env.Events[2].Source = new(hostile)
	env.Events[3].Source = new(hostile)
	env.Events[0].Text = hostile
	env.Annotations[0].Body = hostile

	return env
}

var _ = Describe("WriteHTML", func() {
	It("lays out one page: the viewer's containers, then the export and zone as data, the library and the viewer", func() {
		matchGoldenFile(htmlGoldenDir, "checkout-spike", ".html", elideAssets(page(checkoutSpike(), time.UTC)))
	})

	It("keeps hostile text out of the markup, embedding it only as JSON escaped for HTML", func() {
		matchGoldenFile(htmlGoldenDir, "hostile", ".html", elideAssets(page(hostileExport(), time.UTC)))
	})

	DescribeTable("keeps every script and style element closing where it should, with the export intact in the data block",
		func(env export.Envelope) {
			p := page(env, time.UTC)

			Expect(strings.Count(p, "<script")).To(Equal(4))
			Expect(regexp.MustCompile(`(?i)</script`).FindAllStringIndex(p, -1)).To(HaveLen(4))
			Expect(regexp.MustCompile(`(?i)</style`).FindAllStringIndex(p, -1)).To(HaveLen(1))
			Expect(p).NotTo(ContainSubstring("<img"))
			Expect(p).NotTo(ContainSubstring("<!--"))
			var got export.Envelope
			Expect(json.Unmarshal(jsonBlock(p, "coroner-data"), &got)).To(Succeed())
			Expect(got.Timelines).To(ContainElement(HaveField("Title", hostile)))
			Expect(got.Events).To(ContainElement(HaveField("Source", HaveValue(Equal(hostile)))))
			Expect(got.Events).To(ContainElement(HaveField("Text", hostile)))
			Expect(got.Annotations).To(ContainElement(HaveField("Body", hostile)))
		},
		Entry("one timeline", hostileExport()),
		Entry("an overlay", hostileOverlay()),
	)

	It("has a viewer that puts no export text on the page as HTML", func() {
		viewer, err := os.ReadFile("html/viewer.js")
		Expect(err).NotTo(HaveOccurred())

		Expect(string(viewer)).NotTo(MatchRegexp(`\b(innerHTML|outerHTML|insertAdjacentHTML|createContextualFragment|DOMParser)\b|document\.write|\btitle\s*:`))
	})

	It("has a viewer whose annotations export validates, as a header and comments with UUIDv7 IDs and nine-digit instants", func() {
		// A reviewer downloaded this from the page; the script beside it imports it.
		data := archiveFile("../cli/testdata/script/viewer-annotations.txtar", "inc-4821-annotations-Priya-Shah.jsonl")
		expectValidJSONL(exportSchemas(), data)
		nineDigits := MatchRegexp(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z$`)

		lines := slices.Collect(bytes.Lines(data))
		Expect(len(lines)).To(BeNumerically(">", 1))
		var header map[string]any
		Expect(json.Unmarshal(lines[0], &header)).To(Succeed())
		Expect(header).To(And(HaveKeyWithValue("type", "header"), HaveKeyWithValue("exported_at", nineDigits)))
		for _, line := range lines[1:] {
			var record map[string]any
			Expect(json.Unmarshal(line, &record)).To(Succeed())
			Expect(record).To(And(
				HaveKeyWithValue("type", "annotation"), HaveKeyWithValue("kind", "comment"), HaveKeyWithValue("author", BeAssignableToTypeOf("")),
				HaveKeyWithValue("created_at", nineDigits), HaveKeyWithValue("updated_at", nineDigits),
			))
			text, _ := record["id"].(string)
			id, err := uuid.Parse(text)
			Expect(err).NotTo(HaveOccurred())
			Expect(id.Version()).To(Equal(uuid.Version(7)), text)
			Expect(id.Variant()).To(Equal(uuid.RFC4122), text)
		}
	})

	It("has a stylesheet that paints only with the theme's tokens", func() {
		css, err := os.ReadFile("html/viewer.css")
		Expect(err).NotTo(HaveOccurred())
		paints := regexp.MustCompile(`(?m)^\s*((?:--)?[a-z-]*(?:color|background|border|outline|shadow|fill|stroke|source)[a-z-]*)\s*:\s*([^;]+);`)
		themedOrPlain := `^(?:\s+|,|var\(--[a-z0-9-]+\)|-?[\d.]+(?:px|rem|em|pt|%)?|solid|dashed|dotted|none|transparent|inherit)*$`

		declarations := paints.FindAllStringSubmatch(string(css), -1)
		Expect(len(declarations)).To(BeNumerically(">=", 30))
		for _, d := range declarations {
			Expect(d[2]).To(MatchRegexp(themedOrPlain), d[1])
		}
	})

	It("inlines the vendored library whose SHA-256 its README records", func() {
		library, err := os.ReadFile("html/vendor/vis-timeline-graph2d.min.js")
		Expect(err).NotTo(HaveOccurred())
		readme, err := os.ReadFile("html/vendor/README.md")
		Expect(err).NotTo(HaveOccurred())

		sum := sha256.Sum256(library)
		Expect(string(readme)).To(ContainSubstring(hex.EncodeToString(sum[:])))
	})

	It("embeds the events by occurred at and seq", func() {
		var got export.Envelope
		Expect(json.Unmarshal(jsonBlock(page(checkoutSpike(), time.UTC), "coroner-data"), &got)).To(Succeed())

		Expect(got.Events).To(HaveExactElements(HaveField("ID", "e1"), HaveField("ID", "e2"), HaveField("ID", "e3"), HaveField("ID", "e4")))
	})

	It("names a local zone as the exporter's, not the reader's", func() {
		// time.Local is named Local unless TZ names a zone.
		z := zoneOf(page(checkoutSpike(), time.FixedZone("Local", 2*60*60)))

		Expect(z.Name).To(Equal("the exporter's local zone"))
	})

	It("fails, writing nothing, when an annotation's event is not in the export", func() {
		env := checkoutSpike()
		env.Events = env.Events[:1]
		var b bytes.Buffer

		Expect(export.WriteHTML(&b, env, time.UTC)).To(MatchError(ContainSubstring("e2")))
		Expect(b.Len()).To(BeZero())
	})

	DescribeTable("links and loads nothing from the network",
		func(env export.Envelope) {
			p := page(env, time.UTC)

			Expect(p).NotTo(MatchRegexp(`(?i)\b(src|href)\s*=\s*["']?\s*(https?:)?//`))
			Expect(p).NotTo(MatchRegexp(`(?i)<(link|img|iframe|object|embed)\b|<script[^>]*\bsrc\b`))
		},
		Entry("with URLs in refs and events", checkoutSpike()),
		Entry("with hostile text", hostileExport()),
		Entry("with hostile text in an overlay", hostileOverlay()),
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

	It("fails, writing nothing, when the export holds no timeline", func() {
		env := checkoutSpike()
		env.Timelines = []export.Timeline{}
		var b bytes.Buffer

		Expect(export.WriteHTML(&b, env, time.UTC)).To(MatchError(ContainSubstring("want a timeline")))
		Expect(b.Len()).To(BeZero())
	})

	Describe("an overlay of several timelines", func() {
		It("embeds every timeline, and their events by occurred at, then timeline id, then seq", func() {
			matchGoldenFile(htmlGoldenDir, "overlay", ".html", elideAssets(page(overlay(), time.UTC)))
		})

		It("fails, writing nothing, when an event's timeline is not in the export", func() {
			env := overlay()
			env.Timelines[1].ID = "t3"
			var b bytes.Buffer

			Expect(export.WriteHTML(&b, env, time.UTC)).To(MatchError(ContainSubstring("its timeline t2 is not in the export")))
			Expect(b.Len()).To(BeZero())
		})
	})
})
