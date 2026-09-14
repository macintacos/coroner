package export

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// luminance returns the WCAG 2.2 relative luminance of hex, a #rrggbb colour.
func luminance(hex string) float64 {
	GinkgoHelper()
	channel := func(i int) float64 {
		v, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		Expect(err).NotTo(HaveOccurred())
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}

		return math.Pow((c+0.055)/1.055, 2.4)
	}

	return 0.2126*channel(1) + 0.7152*channel(3) + 0.0722*channel(5)
}

// contrast returns the WCAG 2.2 contrast ratio of two #rrggbb colours.
func contrast(a, b string) float64 {
	GinkgoHelper()
	lighter, darker := luminance(a), luminance(b)
	if lighter < darker {
		lighter, darker = darker, lighter
	}

	return (lighter + 0.05) / (darker + 0.05)
}

// withRole returns the viewer tokens that have role.
func withRole(role tokenRole) []themeToken {
	return slices.DeleteFunc(viewerTokens(), func(t themeToken) bool { return t.Role != role })
}

// pairEntries returns an entry for each foreground on each background, in each theme.
func pairEntries(foregrounds, backgrounds []themeToken) []TableEntry {
	var entries []TableEntry
	for _, fg := range foregrounds {
		for _, bg := range backgrounds {
			entries = append(entries,
				Entry(fmt.Sprintf("%s on %s, light", fg.Name, bg.Name), fg.Color.Light, bg.Color.Light),
				Entry(fmt.Sprintf("%s on %s, dark", fg.Name, bg.Name), fg.Color.Dark, bg.Color.Dark),
			)
		}
	}

	return entries
}

var _ = Describe("The HTML viewer's theme", func() {
	sources := make([]themeToken, len(sourcePalette()))
	for i, c := range sourcePalette() {
		sources[i] = themeToken{Name: fmt.Sprintf("source-%d", i), Color: c}
	}

	It("writes every colour as #rrggbb in both themes", func() {
		for _, t := range append(viewerTokens(), sources...) {
			Expect(t.Color.Light).To(MatchRegexp(`^#[0-9a-f]{6}$`), t.Name)
			Expect(t.Color.Dark).To(MatchRegexp(`^#[0-9a-f]{6}$`), t.Name)
		}
	})

	DescribeTable("gives text at least WCAG AA contrast, 4.5:1",
		func(text, background string) {
			ratio := contrast(text, background)
			GinkgoWriter.Printf("%s on %s: %.2f:1\n", text, background, ratio)
			Expect(ratio).To(BeNumerically(">=", 4.5))
		},
		pairEntries(withRole(roleText), withRole(roleBackground)),
	)

	DescribeTable("gives each source colour at least WCAG's non-text contrast, 3:1",
		func(mark, background string) {
			ratio := contrast(mark, background)
			GinkgoWriter.Printf("%s on %s: %.2f:1\n", mark, background, ratio)
			Expect(ratio).To(BeNumerically(">=", 3))
		},
		pairEntries(sources, withRole(roleBackground)),
	)
})
