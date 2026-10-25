# Brief

*Audience: coding agents changing how coroner looks or reads to people, and the maintainer
who sets its design direction.*

## Subject

coroner records root-cause investigation timelines from the command line, and exports them
for people who were not at the keyboard.

**Audience:** an engineer mid-investigation, hands on the keyboard, adding events from a
terminal as they turn up; and, later, the colleagues who open the export to review what
happened and comment on it.

**The one job:** see what happened when, and how long nothing happened, without leaving
the keyboard.

## Surfaces and modes

| Surface | Where | Mode |
| --- | --- | --- |
| HTML export: timeline, filters, event detail, draft comments | `internal/export/html/`, `internal/export/theme.go` | Read, then comment |
| SVG export, a still picture pasted into other pages | `internal/export/svg/`, `internal/export/svg.go` | Read |
| Terminal viewer (`timeline view`) and picker (`timeline list`) | `internal/viewer/` | Operate |
| Forms (`event add`, `annotation add`, `timeline create`, `timeline edit`) | `internal/cli/add_form.go`, `internal/cli/form.go` | Operate |
| Command output: confirmations, next steps, errors | `internal/cli/output.go`, `internal/cli/errors.go` | Operate |
| Docs site | `docs/`, `zensical.toml` | Read |

## Direction

A quiet, warm tool that gives all its hue to the evidence. The export sits on warm paper
in light and a warm near-black in dark. Each source has its own colour, and the
interface's one vivid accent, a violet kept apart from every source hue, marks only focus,
the selected event and the now line. Surfaces separate by a shift in background, a
hairline and a whisper of depth, with radii nested by size. The system sans carries prose
and Berkeley Mono leads the code stack. Every state change takes 240 ms ease-out, and
reduced motion stops it. The page is keyboard first: each control shows its key, with vim
keys between events. It has a light and a dark theme, following the system, and it makes
no sound: it is a document people share.

In the terminal: lead with what a plain log can't show, the gaps between events, and spend
bold on them alone. Use only the 16 ANSI colour slots, so the user's theme picks every
shade, and never let colour carry meaning alone. Frame forms and errors in rounded
box-drawing, put a Nerd Font glyph beside each field's title, falling back to emoji, and
mark what changed with ✓ and the next command with →. Piped output stays plain.
