package export

import (
	"fmt"
	"strings"
)

// themeColor is a colour as the light and the dark theme show it, each #rrggbb.
type themeColor struct {
	Light, Dark string
}

// themeToken is a colour the HTML viewer's stylesheet reads as var(--Name).
type themeToken struct {
	Name  string
	Color themeColor
}

// viewerTokens returns the HTML viewer's colours. Specs hold ink, muted and accent to WCAG AA
// text contrast on paper, surface and selected in both themes.
func viewerTokens() []themeToken {
	return []themeToken{
		// paper is the page.
		{"paper", themeColor{"#f6f7f5", "#121517"}},
		// surface sets a control, a well or a row apart from the page.
		{"surface", themeColor{"#eaecea", "#1b1f22"}},
		// selected is behind the selected event.
		{"selected", themeColor{"#dce7ef", "#1d3140"}},
		// ink is body text.
		{"ink", themeColor{"#1b1f22", "#e3e7e9"}},
		// muted is secondary text: labels, the export stamp, hints.
		{"muted", themeColor{"#545c63", "#9ba4ab"}},
		// rule is borders and grid lines.
		{"rule", themeColor{"#d3d8d6", "#30373c"}},
		// accent is links, focus rings and the selection outline.
		{"accent", themeColor{"#1f5f8b", "#7fb6dc"}},
	}
}

// sourcePalette returns the colours sources take in order of first appearance, wrapping
// after the last, so a timeline's sources keep their colours on every export. Specs hold each
// to WCAG non-text contrast on paper and surface in both themes; a source's name always
// shows beside its colour.
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
// reader's system asks for it, light in print, and --source for each .source-<i>.
func themeCSS() string {
	light := declarations(func(c themeColor) string { return c.Light })
	dark := declarations(func(c themeColor) string { return c.Dark })

	var css strings.Builder
	fmt.Fprintf(&css, ":root {\n  color-scheme: light;\n%s}\n", light)
	fmt.Fprintf(&css, ":root[data-theme=\"dark\"] {\n  color-scheme: dark;\n%s}\n", dark)
	fmt.Fprintf(&css, "@media (prefers-color-scheme: dark) {\n:root:not([data-theme=\"light\"]) {\n  color-scheme: dark;\n%s}\n}\n", dark)
	// :root:root outranks both dark rules above.
	fmt.Fprintf(&css, "@media print {\n:root:root {\n  color-scheme: light;\n%s}\n}\n", light)
	for i := range sourcePalette() {
		fmt.Fprintf(&css, ".source-%d { --source: var(--source-%d); }\n", i, i)
	}

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
