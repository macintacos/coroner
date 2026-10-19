package export_test

import (
	"bytes"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/yuin/goldmark/ast"
	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Decode, on any input", func() {
	It("never panics, and turns each line after a valid header into a record or a LineError naming it", func() {
		sample := strings.Split(strings.TrimSuffix(sampleExport(), "\n"), "\n")
		header, bodies := sample[0], sample[1:]
		rapid.Check(GinkgoT(), func(t *rapid.T) {
			line := rapid.OneOf(
				rapid.SampledFrom(bodies),
				rapid.Custom(func(t *rapid.T) string {
					body := rapid.SampledFrom(bodies).Draw(t, "body")
					return body[:rapid.IntRange(0, len(body)-1).Draw(t, "cut")]
				}),
				rapid.Map(rapid.String(), func(s string) string { return strings.ReplaceAll(s, "\n", "") }),
			)
			lines := append([]string{header}, rapid.SliceOf(line).Draw(t, "lines")...)

			records, err := export.Decode(strings.NewReader(strings.Join(lines, "\n") + "\n"))

			var named []int
			if decodeErr, ok := errors.AsType[*export.DecodeError](err); ok {
				for _, e := range decodeErr.Lines {
					named = append(named, e.Line)
				}
			} else if err != nil {
				t.Fatalf("Decode = %v; want nil or a *DecodeError", err)
			}
			if len(records)+len(named) != len(lines) {
				t.Fatalf("Decode gave %d records and %d line errors for %d lines", len(records), len(named), len(lines))
			}
			for i, line := range named {
				if line < 2 || line > len(lines) || (i > 0 && line <= named[i-1]) {
					t.Fatalf("line errors name lines %v of %d", named, len(lines))
				}
			}
		})
	})
})

var _ = Describe("WriteMarkdown, on any event text", func() {
	It("shows the text as written on one line, under CommonMark and GFM alike", func() {
		token := rapid.SampledFrom([]string{
			"https://a.example/", "http://b.example/x?y=1&z", "[", "]", "(", ")", "!", `\`, "*", "_", "`", "<", ">",
			"&", "lt;", "#", "-", "+", "1.", "|", "~", "=", "a", "b ", " c",
		})
		rapid.Check(GinkgoT(), func(t *rapid.T) {
			text := strings.Join(rapid.SliceOfN(token, 1, 12).Draw(t, "tokens"), "")
			if strings.TrimSpace(text) != text {
				t.Skip("CommonMark trims a paragraph's leading and trailing spaces")
			}
			env := envelope(
				export.Timeline{ID: "t1", Name: "any", Refs: []string{}},
				[]export.Event{{
					ID: "e1", Seq: new(int64(1)), OccurredAt: utc("2026-09-11T03:14:07Z"), Precision: timeline.PrecisionSecond,
					Tags: []string{}, Text: text,
				}},
				[]export.Annotation{},
			)
			var b bytes.Buffer
			err := export.WriteMarkdown(&b, env, export.MarkdownOptions{Zone: time.UTC})
			if err != nil {
				t.Fatalf("WriteMarkdown: %v", err)
			}
			source := b.Bytes()

			for _, doc := range []ast.Node{parseMarkdown(source), parseGFM(source)} {
				if got := visibleText(eventItem(doc), source); got != "03:14:07 · "+text {
					t.Fatalf("event item reads %q; want %q\n%s", got, "03:14:07 · "+text, source)
				}
			}
		})
	})
})

// eventItem returns the first item of doc's last list, the first event's, without listItems'
// Gomega assertions, which rapid cannot shrink.
func eventItem(doc ast.Node) ast.Node {
	var list ast.Node
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Kind() == ast.KindList {
			list = c
		}
	}

	return list.FirstChild()
}
