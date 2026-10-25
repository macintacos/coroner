# Tokens

*Audience: coding agents changing how coroner looks or reads to people, and the maintainer
who sets its design direction.*

Where each design value lives. Change values there, never here.

## Colour: the exports

`viewerTokens()` and `sourcePalette()` in
[`internal/export/theme.go`](../internal/export/theme.go) hold every colour the HTML page
and the SVG picture show, each with a light and a dark value. `themeCSS()` writes them
into the page as custom properties for light, dark and print, and
`internal/export/theme_internal_test.go` holds them to the contrast floors.
[`viewer.css`](../internal/export/html/viewer.css) names no colour.

| Role | Token |
| --- | --- |
| Canvas | `paper` |
| Surface: a control, a well or a row set apart from the page | `surface` |
| Selection band, behind the selected event | `selected` |
| Text | `ink` |
| Secondary text: labels, the export stamp, hints | `muted` |
| Separator: borders and grid lines | `rule` |
| Accent: focus, the selected event's outline, the now line | `accent` |
| Data: each source's colour, by first appearance | `sourcePalette()`, as `--source` |

The page has no destructive or status colour. One that arrives is a new role in
`viewerTokens()`, never a literal in the stylesheet.

The SVG takes the light `ink`, `muted` and `rule` and the light source colours, on the
white `svgBackground` in [`internal/export/svg.go`](../internal/export/svg.go).

## Colour: the terminal

Terminal output has no tokens. It uses the 16 ANSI colour slots, so the user's terminal
theme supplies every shade.

- The viewer's styles: `newStyles()` in
  [`internal/viewer/render.go`](../internal/viewer/render.go), which assigns the slots to
  sources, tags, gaps, evidence, failures, notices and an overlay's timelines.
- The picker's list, help and filter styles: `listStyles()`, `helpStyles()` and
  `filterStyles()` in [`internal/viewer/picker.go`](../internal/viewer/picker.go).
- The forms' frame: `frameBox()` and `frame()` in
  [`internal/cli/add_form.go`](../internal/cli/add_form.go), and the session summary's
  `newNoteStyles()` beside them.
- Command output: the error label in `writeError()` in
  [`internal/cli/errors.go`](../internal/cli/errors.go), and `colorEnabled()` beside it,
  the one check for whether a stream gets colour.

## Colour: the docs site

The `[[project.theme.palette]]` entries in [`zensical.toml`](../zensical.toml).

## Type, spacing, radius and motion

All in the `:root` block of
[`internal/export/html/viewer.css`](../internal/export/html/viewer.css):

- **Faces:** `--font-sans` and `--font-mono`. No font file ships, since the page loads
  nothing over the network, so each stack falls back to the reader's platform.
- **Size:** the `--text-*` ramp from a 15px body, and `--text-print`.
- **Spacing:** the `--space-*` scale and `--measure`.
- **Radius:** `--radius-xs`, `--radius-sm` and `--radius-md`.
- **Motion:** none yet; [`decisions.md`](decisions.md) has the open item.

The SVG sets its face and sizes in
[`internal/export/svg/timeline.svg.tmpl`](../internal/export/svg/timeline.svg.tmpl). The
terminal uses the user's own font.

## Icons

- The forms' field icons: `fieldIcon()` in
  [`internal/cli/form.go`](../internal/cli/form.go).
- The timeline mark in a form's frame: `formFrame.edges()` in
  [`internal/cli/add_form.go`](../internal/cli/add_form.go).
- The suggestions mark: `suggestionsLabel` in
  [`internal/cli/form_match.go`](../internal/cli/form_match.go).
