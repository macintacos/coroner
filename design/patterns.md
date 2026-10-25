# Patterns

*Audience: coding agents changing how coroner looks or reads to people, and the maintainer
who sets its design direction.*

How coroner builds things. Values live where [`tokens.md`](tokens.md) says. The rules for
the code behind each surface live in [`rules/export.md`](../rules/export.md) and
[`rules/viewer.md`](../rules/viewer.md), and command wording follows
[the command checklist](../docs/contributing/command-checklist.md).

## HTML export

### Colour and emphasis

- Hue belongs to the evidence. Each source keeps its colour, and its name always shows
  beside it.
- The accent is used verbatim and never lightened. It fails the 4.5:1 text floor, so it
  goes only on non-text marks: the focus ring, the selected event's outline and the now
  line.
- Links are ink, with an accent underline.
- The selected event sits on the `selected` band. A source colour never marks selection or
  focus.
- Secondary text is `muted`, in both themes.

### Surfaces, type and shape

- A surface sets itself apart by a shift in background, a `rule` hairline and a whisper of
  depth. The timeline frame and the event detail panel take depth; controls take a very
  subtle depth gradient and nothing louder.
- Radii nest by size: 12px panels (`--radius-md`), 6px controls (`--radius-sm`), 3px
  swatches and marks (`--radius-xs`).
- The system sans carries prose. Berkeley Mono leads the code stack, and readers without
  it get their platform's mono.
- Page structure is left-aligned.
- An empty state is one plain sentence, as the SVG's "No events." is.

### Motion

- Every state change takes 240 ms ease-out (`--dur-state`): hover, focus, selection, and
  the detail panel arriving.
- Reduced motion stops every transition and animation.

### Keyboard

- Keyboard first. Platform conventions always work, and vim keys sit on top where they
  don't collide with typing: `j`/`k` between events, `/` to search, `?` for every key,
  `esc` to clear the selection.
- Each control shows the key that activates it on the control itself. The hints are on by
  default, a setting hides them, and touch screens never show them.

### Themes, print and sound

- A light and a dark theme, following the system, with the page's own toggle remembered
  per reader.
- Print is always light.
- No sound ([`exceptions.md`](exceptions.md#no-sound-on-the-export-page)).

## SVG export

- A still picture on a white card, so it reads on any page it is pasted into. It takes the
  light tokens and labels each lane in its source's colour.

## Terminal

### Colour and emphasis

- The 16 ANSI slots only, so the user's theme picks every shade.
- Colour never carries meaning alone: evidence and comments, gaps, day breaks, the current
  timeline, an overlay's timelines and the cursor each have a glyph or word as well.
- The viewer spends bold on one element: the gaps, the silences between events that a
  plain log can't show.
- The cursor row is reverse video.
- Without colour (`--no-color`, `NO_COLOR`, `TERM=dumb`, or output that is not a terminal)
  output is plain text.

### Frames and icons

- Forms sit in a rounded box-drawing frame: the title set into the top edge, the timeline
  and its key at the top right, the running count at the bottom right.
- A Nerd Font glyph sits beside each field's title, the frame's timeline and the
  suggestions. Where coroner can't tell that the terminal draws Nerd Font glyphs, they
  fall back to the emoji, which every terminal draws two cells wide.
- Box-drawing rules divide the viewer's days and gaps.
- Each key hint names its key beside a one-word action, as `enter expand`.

### Command output

- Say what changed, then suggest the next command.
- On a colour terminal, ✓ marks each change, → marks the next command, and an error sits
  in a rounded red frame, headed `✗ Error`, with its hint inside on a → line.
- Piped output, `NO_COLOR` and the JSON error envelope stay plain: no symbols and no
  frame.

## Docs site

- Zensical's stock layout, with its palette taken from the export's: warm paper and
  near-black grounds and the violet accent.
