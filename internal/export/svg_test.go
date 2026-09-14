package export_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

const svgGoldenDir = "testdata/svg"

func svg(env export.Envelope, zone *time.Location) string {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(export.WriteSVG(&b, env, zone)).To(Succeed())

	return b.String()
}

// svgDocument is what an SVG export holds once parsed as XML: its root element, and the text
// of every title and text element.
type svgDocument struct {
	root           xml.Name
	titles, labels []string
}

// parseSVG parses picture with encoding/xml's strict decoder, failing on anything that is not
// well-formed XML, on an element the picture never draws, and on an href or style attribute.
func parseSVG(picture string) svgDocument {
	GinkgoHelper()
	var doc svgDocument
	var open []string
	decoder := xml.NewDecoder(strings.NewReader(picture))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		switch t := token.(type) {
		case xml.StartElement:
			if doc.root.Local == "" {
				doc.root = t.Name
			}
			Expect(t.Name.Local).To(BeElementOf("svg", "title", "rect", "text", "g", "line", "circle"))
			for _, a := range t.Attr {
				Expect(a.Name.Local).NotTo(BeElementOf("href", "style"), "an attribute of %s", t.Name.Local)
			}
			open = append(open, t.Name.Local)
		case xml.EndElement:
			open = open[:len(open)-1]
		case xml.CharData:
			switch {
			case len(open) == 0:
			case open[len(open)-1] == "title":
				doc.titles = append(doc.titles, string(t))
			case open[len(open)-1] == "text":
				doc.labels = append(doc.labels, string(t))
			}
		}
	}
	Expect(doc.root).To(Equal(xml.Name{Space: "http://www.w3.org/2000/svg", Local: "svg"}))

	return doc
}

// matchSVGGolden compares picture with testdata/svg/<name>.svg, after checking it is
// well-formed.
func matchSVGGolden(name, picture string) {
	GinkgoHelper()
	parseSVG(picture)
	matchGoldenFile(svgGoldenDir, name, ".svg", picture)
}

// hostileSVG carries markup, entities, quotes, CDATA ends, control characters XML forbids and
// a 10 KB line in the title, refs, a source and event text.
func hostileSVG() export.Envelope {
	env := hostileLabels()
	env.Timelines[0].Title = hostile
	env.Timelines[0].Refs = []string{hostile}
	env.Events[0].Source = new(hostile)
	env.Events = append(env.Events, export.Event{
		ID: "ez", Seq: new(int64(99)), OccurredAt: utc("2026-09-11T03:15:00Z"), Precision: timeline.PrecisionSecond, Tags: []string{},
		Text: `<script>alert(1)</script> "double" 'single' ]]> bell` + "\x01 escape\x1b",
	})

	return env
}

var _ = Describe("WriteSVG", func() {
	It("draws a lane per source, bars for intervals, points for exact instants and hollow marks for guessed or coarse times", func() {
		matchSVGGolden("checkout-spike", svg(checkoutSpike(), time.UTC))
	})

	It("draws every precision, from day bands to nanosecond points", func() {
		matchSVGGolden("precisions", svg(precisions(), time.UTC))
	})

	It("shows times on the wall clock of the zone it names", func() {
		picture := svg(checkoutSpike(), newYork())

		Expect(parseSVG(picture).labels).To(ContainElement("Times in America/New_York"))
		matchSVGGolden("new-york", picture)
	})

	It("draws a timeline with no events as its header and a note", func() {
		matchSVGGolden("empty", svg(envelope(export.Timeline{ID: "t1", Name: "quiet", Refs: []string{}}, nil, nil), time.UTC))
	})

	It("keeps hostile text as text in well-formed XML, with the full text in tooltips", func() {
		picture := svg(hostileSVG(), time.UTC)

		doc := parseSVG(picture)
		Expect(doc.titles).To(ContainElement(hostile))
		Expect(doc.titles).To(ContainElement(HaveSuffix(strings.Repeat("ab:c#", 2000))))
		Expect(doc.labels).To(ContainElement(ContainSubstring(`<script>alert(1)</script> "double"`)))
		Expect(doc.titles).To(ContainElement(ContainSubstring("bell� escape�")))
		matchSVGGolden("hostile", picture)
	})

	It("writes the same bytes for the same export", func() {
		Expect(svg(checkoutSpike(), time.UTC)).To(Equal(svg(checkoutSpike(), time.UTC)))
	})

	DescribeTable("fails, writing nothing, unless the export holds one timeline",
		func(timelines []export.Timeline) {
			env := checkoutSpike()
			env.Timelines = timelines
			var b bytes.Buffer

			Expect(export.WriteSVG(&b, env, time.UTC)).To(MatchError(ContainSubstring("want one timeline")))
			Expect(b.Len()).To(BeZero())
		},
		Entry("none", []export.Timeline{}),
		Entry("two", []export.Timeline{{ID: "t1", Name: "a"}, {ID: "t2", Name: "b"}}),
	)
})
