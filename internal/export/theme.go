package export

import (
	"fmt"
	"strings"
)

// themeColor is a colour as the light and the dark theme show it, each #rrggbb.
type themeColor struct {
	Light, Dark string
}

// tokenRole is what a theme token paints, which decides the contrast it must keep.
type tokenRole int

const (
	roleBackground tokenRole = iota
	roleText
	// roleLine is borders and grid lines, which have no contrast floor.
	roleLine
)

// themeToken is a colour the HTML viewer's stylesheet reads as var(--Name).
type themeToken struct {
	Name  string
	Role  tokenRole
	Color themeColor
}

// viewerTokens returns the HTML viewer's colours. Specs hold every text token to WCAG AA text
// contrast on every background token in both themes.
func viewerTokens() []themeToken {
	return []themeToken{
		// paper is the page.
		{"paper", roleBackground, themeColor{"#f6f7f5", "#121517"}},
		// surface sets a control, a well or a row apart from the page.
		{"surface", roleBackground, themeColor{"#eaecea", "#1b1f22"}},
		// selected is behind the selected event.
		{"selected", roleBackground, themeColor{"#dce7ef", "#1d3140"}},
		// ink is body text.
		{"ink", roleText, themeColor{"#1b1f22", "#e3e7e9"}},
		// muted is secondary text: labels, the export stamp, hints.
		{"muted", roleText, themeColor{"#545c63", "#9ba4ab"}},
		// rule is borders and grid lines.
		{"rule", roleLine, themeColor{"#d3d8d6", "#30373c"}},
		// accent is links, focus rings and the selection outline.
		{"accent", roleText, themeColor{"#1f5f8b", "#7fb6dc"}},
	}
}

// sourcePalette returns the colours sources take in order of first appearance, wrapping
// after the last. Specs hold each to WCAG non-text contrast on every background token in both
// themes; a source's name always shows beside its colour.
func sourcePalette() []themeColor {
	return []themeColor{
		{"#0b6aa8", "#5aaef0"}, // blue
		{"#b35c00", "#f0a050"}, // orange
		{"#1f7a4d", "#5cc28e"}, // green
		{"#b0306e", "#e97fb4"}, // magenta
		{"#6a4fb0", "#a996f0"}, // violet
		{"#00797a", "#4fc4c4"}, // teal
		{"#876a00", "#d8b64a"}, // ochre
		{"#b3261e", "#f28b82"}, // red
	}
}

// themeCSS returns the stylesheet rules that declare each viewer token and source colour as
// a custom property: light by default, dark when the page's data-theme or, without one, the
// reader's system asks for it, light in print, and --source for each .source-<i> and for
// .source-none.
func themeCSS() string {
	light := declarations(func(c themeColor) string { return c.Light })
	dark := declarations(func(c themeColor) string { return c.Dark })

	var css strings.Builder
	fmt.Fprintf(&css, ":root {\n  color-scheme: light;\n%s}\n", light)
	fmt.Fprintf(&css, ":root[data-theme=\"dark\"] {\n  color-scheme: dark;\n%s}\n", dark)
	fmt.Fprintf(&css, "@media (prefers-color-scheme: dark) {\n:root:not([data-theme=\"light\"]) {\n  color-scheme: dark;\n%s}\n}\n", dark)
	// :root:root:root outranks both dark rules wherever it is written.
	fmt.Fprintf(&css, "@media print {\n:root:root:root {\n  color-scheme: light;\n%s}\n}\n", light)
	for i := range sourcePalette() {
		fmt.Fprintf(&css, ".source-%d { --source: var(--source-%d); }\n", i, i)
	}
	css.WriteString(".source-none { --source: var(--muted); }\n")

	return css.String()
}

// declarations returns a custom property declaration per viewer token and per source colour,
// one a line, with pick choosing the theme's value.
func declarations(pick func(themeColor) string) string {
	var lines strings.Builder
	for _, t := range viewerTokens() {
		fmt.Fprintf(&lines, "  --%s: %s;\n", t.Name, pick(t.Color))
	}
	for i, c := range sourcePalette() {
		fmt.Fprintf(&lines, "  --source-%d: %s;\n", i, pick(c))
	}

	return lines.String()
}
