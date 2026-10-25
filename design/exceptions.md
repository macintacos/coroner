# Exceptions

*Audience: coding agents changing how coroner looks or reads to people, and the maintainer
who sets its design direction.*

The places where coroner departs from the `/taste` defaults its direction starts from, on
purpose. Floors never appear here.

## No sound on the export page

**Default:** sound is part of the polish on web apps, on by default with a setting to turn
it off.

**coroner:** the HTML export makes no sound. It is a document people pass around and read,
not an app they work in, and every other surface is terminal output. Set on 2026-10-24.

## Emoji where Nerd Font glyphs can't be confirmed

**Default:** Nerd Font glyphs sit beside the words in terminal output.

**coroner:** the forms show Nerd Font glyphs only where coroner can tell the terminal
draws them, and fall back to emoji everywhere else. coroner ships to terminals and fonts
it doesn't control, where a missing Nerd Font draws every glyph as an empty box, while
emoji draw everywhere. Chosen over glyphs everywhere and over no icons on 2026-10-24.
