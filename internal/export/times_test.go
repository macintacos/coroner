package export_test

import (
	"regexp"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timetext"
)

var _ = Describe("Markdown and SVG", func() {
	DescribeTable("show an event's time as timetext reads it, and Markdown files it under its day",
		func(text string, zone func() *time.Location) {
			env := times()
			i := slices.IndexFunc(env.Events, func(e export.Event) bool { return e.Text == text })
			Expect(i).NotTo(Equal(-1))
			r := timetext.Read(env.Events[i].Domain(), zone())
			want := []string{startText(r)}
			if r.End != nil {
				want = append(want, r.End.Date)
			}

			named := regexp.MustCompile(`\b` + text + `\b`)
			heading, item := markdownItem(markdown(env, export.MarkdownOptions{Zone: zone()}), named)
			titles := parseSVG(svg(env, zone())).titles
			titleIndex := slices.IndexFunc(titles, named.MatchString)
			Expect(titleIndex).NotTo(Equal(-1))
			title := titles[titleIndex]

			Expect(heading).To(HavePrefix("## " + r.Day + " ("))
			for _, w := range want {
				Expect(item).To(ContainSubstring(w))
				Expect(title).To(ContainSubstring(w))
			}
		},
		Entry("an hour recorded at +05:30, in UTC", "E3", func() *time.Location { return time.UTC }),
		Entry("an hour recorded at +05:30, in New York", "E3", newYork),
		Entry("20:30Z recorded at +05:30, in UTC", "E13", func() *time.Location { return time.UTC }),
		Entry("20:30Z recorded at +05:30, in New York", "E13", newYork),
		Entry("a day interval, in UTC", "E2", func() *time.Location { return time.UTC }),
		Entry("a day interval, in New York", "E2", newYork),
	)
})

// startText returns what both surfaces show of r's start: its date when it is filed under
// another day, then its clock and its offset when it is read in its own; a day shows only its
// date.
func startText(r timetext.Reading) string {
	var parts []string
	if r.Start.Date != r.Day {
		parts = append(parts, r.Start.Date)
	}
	if r.Start.Clock != "" {
		parts = append(parts, r.Start.Clock)
		if r.Start.OwnOffset {
			parts = append(parts, r.Start.Offset)
		}
	}

	return strings.Join(parts, " ")
}

// markdownItem returns the day heading above the first line of outline that named matches,
// and that line.
func markdownItem(outline string, named *regexp.Regexp) (string, string) {
	GinkgoHelper()
	var heading string
	for line := range strings.SplitSeq(outline, "\n") {
		if strings.HasPrefix(line, "## ") {
			heading = line
		}
		if named.MatchString(line) {
			return heading, line
		}
	}
	Fail("no line names " + named.String())

	return "", ""
}
