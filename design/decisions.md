# Decisions

*Audience: coding agents changing how coroner looks or reads to people, and the maintainer
who sets its design direction.*

Open items first, then the log, newest first.

## Open: where the code still differs

- **Control boundaries miss the 3:1 floor.** Inputs and buttons on the HTML export draw
  their edge in `rule`, which sits at 1.2 to 1.5:1 on `paper` and `surface` in both
  themes, under the 3:1 that non-text UI needs. Grid lines can stay at `rule`; control
  edges need a role of their own that clears 3:1, held there by
  `internal/export/theme_internal_test.go`.
- **Export palette.** `viewerTokens()` in `internal/export/theme.go` takes the chosen
  values, then `accent` becomes a non-text role held to 3:1 rather than 4.5:1, and
  `viewer.css` draws links in `ink` with an `accent` underline.

  | Token | Light | Dark |
  | --- | --- | --- |
  | `paper` | `#fbf8f2` | `#0c0a08` |
  | `surface` | `#f1ece2` | `#17130f` |
  | `selected` | `#f8e7f5` | `#261428` |
  | `ink` | `#1d1915` | `#f4efe7` |
  | `muted` | `#5c544a` | `#ab9f90` |
  | `rule` | `#e0d8cb` | `#2d2720` |
  | `accent` | `#d21afd` | `#d21afd` |

  In the previews these held `ink` at 14.7:1 or more, `muted` at 6.2:1 or more, and every
  source colour at 3.9:1 or more on every background, with `accent` at 3.3:1 in light and
  4.3:1 in dark.
- **Shape, depth and motion.** `viewer.css` moves its radii to 3, 6 and 12px, gives the
  timeline frame and the event detail panel depth, puts Berkeley Mono first in
  `--font-mono`, and adds `--dur-state` (240 ms) with ease-out to every state change, off
  under reduced motion.
- **Keys on the export page.** `viewer.js` gains `j`/`k`, `/`, `?` and `esc`, shows each
  control's key on the control, hides the hints behind a setting and on touch screens.
- **Viewer bold.** `newStyles()` in `internal/viewer/render.go` drops bold from the
  timeline name, the day breaks and the times, and keeps it on the gaps.
- **Form icons.** `fieldIcon()`, `formFrame.edges()` and `suggestionsLabel` switch to Nerd
  Font glyphs where coroner can tell the terminal draws them. How it tells is still to
  decide: the terminals that bundle the symbols themselves, such as Ghostty and WezTerm,
  are the likely start, and any rule needs a way to force either choice.
- **Command output.** On a colour terminal, `notify()` in `internal/cli/output.go` marks
  each change with ✓ and each next command with →, and `writeError()` in
  `internal/cli/errors.go` frames the error. The change goes through
  [the command checklist](../docs/contributing/command-checklist.md), and docs pages that
  show this output under a `console` prompt change with it.
- **Docs site.** `zensical.toml` takes its palette from the export's tokens.

## 2026-10-24: direction set

Chosen through side-by-side previews of the real export page and captured terminal output
([the previews](https://claude.ai/artifact/36BG8e2PbyLBQoDLXufByE)).

- **Export palette:** warm paper and a warm near-black, with one vivid violet accent used
  verbatim on non-text marks only. Chosen over the cool slate grounds, whose blue accent
  was almost the first source's blue (ΔE 5), and over ink chrome with no accent.
- **Shape and motion:** radii nested at 12, 6 and 3px, a hairline plus a whisper of depth,
  and 240 ms ease-out on every state change. Chosen over the flat 5, 3 and 2px page with
  no transitions.
- **Keyboard:** the export page shows each control's key and takes vim keys between
  events. Chosen over the mouse-first page.
- **Viewer emphasis:** bold only on the gaps. Chosen over bold on the name, day breaks,
  times and gaps together, and over bold on the times alone.
- **Form icons:** Nerd Font glyphs, falling back to emoji. Chosen over emoji alone and
  over no icons. See
  [`exceptions.md`](exceptions.md#emoji-where-nerd-font-glyphs-cant-be-confirmed).
- **Command output:** ✓ for each change, → for the next command, and an error in a rounded
  frame, on a colour terminal only. Chosen over plain sentences and a red `Error:` label.
- **Docs site:** in scope, with its palette aligned to the export's. Chosen over the stock
  blue grey and amber.
- **Kept from the defaults:** a light and a dark theme following the system, the ANSI
  slots only in the terminal, and no sound
  ([`exceptions.md`](exceptions.md#no-sound-on-the-export-page)).
