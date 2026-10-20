# Add events with the form

*Audience: people who use coroner and want to add events at a terminal without typing
flags.*

`coroner event add` opens a form when you run it on a terminal without the event's text.
The form asks for the same values the flags take, and prints the command each entry
equals, so you can repeat it in a script.

## Open the form

Run `event add` with no text, or pass `--interactive` to open the form even when you give
some flags:

```sh
coroner event add
coroner event add --source mongod --interactive
```

A value you pass as a flag fills its field. The form never opens without a terminal, or
under `--no-input` or `CORONER_NO_INPUT`. Under `--json` only `--interactive` opens it.

## Fill in the fields

The form draws in a frame titled 📝 New event, at most 80 columns wide, and puts an icon
before each field's title. It first asks for the five fields most events use:

- **What happened:** the event's text.
- **When:** the time it happened, in any form `coroner help time-input` lists. The form
  previews the time it reads as you type. Leave it empty to mean now.
- **Source:** the system, service or person it came from. As you type, the form marks the
  name ✅ Existing source when coroner already has that source, or ✨ New source when the
  event would create it. `annotation add`'s form marks its Source field the same way.
- **Tags:** keywords such as `phase:detect`, added one at a time with `enter`.
- **Files:** files to attach, such as screenshots, one path at a time.

Source and Tags show a `💡 Suggestions:` row of the values on the timeline's latest events
first, then every other one, while the values a field already holds are each marked `✓`.
Once you type, the row shows the ones that start with what you typed, beside the inline
suggestion `ctrl+e` takes.

Files completes the path you type. Once you type, the row lists the entries of the typed
path's directory, relative to where you run coroner, absolute, or in your home directory
as `~/`, that start with the typed name, matching case, and a directory ends in `/`.
`ctrl+e` takes the inline one, so you can keep completing inside a directory. Names
starting with `.` show once you type the `.`. A path starting with `~/` attaches the file
in your home directory, and the form shows and echoes its full path; `~user/` is not
expanded. A file that does not exist is reported when you submit, as `--attach` reports
it, and the form asks the entry again. `annotation add`'s form completes its attach field
the same way.

These keys move through the form:

| Key                 | What it does                                                    |
| ------------------- | --------------------------------------------------------------- |
| `enter`             | Go to the next field; in Tags or Files, add the value typed, or move on from an empty one. |
| `ctrl+e`            | Take the field's suggestion, such as a source already in use or a file path. |
| `↑` / `↓`           | Cycle the matches while you type, or pick a value in Tags or Files while nothing is typed. |
| `ctrl+n` / `ctrl+p` | Cycle the matches.                                              |
| `backspace`         | Remove the picked value.                                        |
| `y` / `n`           | Answer Yes or No, as to More options?                           |
| `shift+tab`         | Go back to the previous field.                                  |
| `esc`               | Drop the entry, and add nothing.                                |
| `ctrl+c`            | Stop coroner, which exits as interrupted.                       |

## Use More options

After Files, the form asks **More options?**, which defaults to No. Its description lists
the fields it holds: tz, ended, approximate, url, author and dry-run, the same values as
the flags of those names. The form never asks for an id; an `--id` given with
`--interactive` applies to the first entry only.

- Answer Yes to fill them in. The tz field comes first, and previews When's time read in
  the zone you type.
- The author field shows your default author, the config file's `author` and then `USER`,
  as a placeholder.
- When one of those fields already holds a value, from a flag such as `--url` or carried
  from the entry before, More options opens Yes by itself.
- Answer No to add the event without any of them, even one that held a value, except the
  zone: the times the form shows are already read in it.

## Add several events in a row

After each event, the form asks **Add another event?** Answer Yes to open the next entry
with these values carried from the last one:

- the timeline
- the source
- the tags
- the zone
- the author
- the dry run

The text, time, end, approximate mark, URL and files start empty each time. To drop a
carried tag, pick it with `↑` and press `backspace`.

Answer No, or press `esc`, to stop. Each form clears from the screen when it ends, so only
what coroner prints stays. When you added at least one event, coroner then lists them: the
timeline, each event's seq, time and text, and the command that shows the timeline. A dry
run, or an `--id` already stored, adds nothing, so the list leaves it out. `-q` silences
the list. `annotation add`'s form ends the same way, listing each annotation's kind and
body.

```text
Equivalent command: coroner event add 'Election timeout on db-3' -t inc-4821 --at 2026-10-20T16:05:00Z

✓ Added 2 events to timeline `inc-4821`
  #3  2026-10-20T16:00:00Z  Primary stepped down
  #4  2026-10-20T16:05:00Z  Election timeout on db-3

  View the timeline: coroner timeline view -t inc-4821
```

## Repeat an entry from a script

Each entry you submit prints the flags-only command it equals, after
`Equivalent command:`. Run that line to add the same event without the form, for example
from a script.
