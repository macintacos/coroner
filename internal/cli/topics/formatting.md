# Output formats and what scripts can rely on

*Audience: anyone reading coroner's output from a script or an agent.*

## Choosing a format

`--format text|json|jsonl` picks the format, plus `md`, `mermaid` and `html` on
`coroner timeline export`, and `--json` is short for `--format json`. Text is the default,
and it stays the default when output is piped: coroner never switches to JSON on its own.
A command exits 2 when asked for a format it cannot print. On a terminal,
`coroner timeline view` opens an interactive viewer; `--format text` or a pipe prints
plain text instead. On a terminal, `coroner event add` and `coroner annotation add` open a
form when their text, body or `--kind` is missing, `coroner timeline create` when its name
is missing, `coroner timeline edit` when no change is given, and each with
`--interactive`. Under `--json` a form opens only with `--interactive`. `--no-input` or
`CORONER_NO_INPUT` keeps every form, the `coroner timeline list` picker and the viewer
closed: a command line missing a required value exits 2 instead of asking.

- `text` is for people.
- `json` is one JSON document, indented when stdout is a terminal and compact otherwise.
- `jsonl` is one JSON object per line.
- `md` is a CommonMark outline for people to paste into a document or chat, with times in
  UTC unless `--tz` names another zone, each labelled with its zone.
- `mermaid` is a Mermaid gantt diagram of the events, a section per source, for GitHub,
  GitLab, Linear, Obsidian or Notion to draw, in the same zone as `md`. `--diagram`
  appends it to `md`. Past 50,000 characters, which most hosts refuse to draw, coroner
  warns on stderr.
- `html` is one self-contained page to open in a browser or attach to a ticket: the events
  on a timeline with a row per source, and an event's full text and annotations when you
  select it. Its source, tag, text and time filters select what `coroner event list`'s
  flags do, and panning or zooming the timeline sets the time filter to what it shows. Its
  link keeps the filters, that window included, and the selected event for sharing, and
  Export PNG saves the timeline as shown. It loads nothing over the network, shows times
  in the zone `md` does, and is written to the file `-o` names, or to stdout with `-o -`.

## What JSON output promises

- Keys are snake_case, and every key is always present: an empty value is `null`, never a
  missing key.
- Under `json`, a collection is wrapped in an object that says whether it was cut short
  (`truncated`) and how many items matched (`total`).
- Under `jsonl`, each item of a collection is its own line, with no wrapping object. A
  limit that cuts the collection short says so on stderr, so scripts that need `truncated`
  or `total` use `json`.
- Instants are fixed-width RFC 3339 in UTC, such as `2026-09-12T14:05:03.000000000Z`. See
  `coroner help time`.
- Each JSONL line is an object whose `type` key says what it holds.
- `coroner timeline export` writes the export format instead: under `json` one envelope
  with no `truncated` or `total`, and under `jsonl` a `header` line before the records.
  Its `query.limit` records a limit, and the command says on stderr when a limit cut the
  export short. The export format reference,
  <https://github.com/macintacos/coroner/blob/main/docs/reference/export-format.md>,
  describes both.

## What consumers must do

- Ignore keys you don't know: new ones are added over time.
- Tolerate enum values you don't know, such as a new `type` or error `kind`.
- Never parse text output or the layout of exported Markdown, Mermaid or HTML. None of
  them is a contract. An HTML export's `coroner-data` script element holds its `json`
  export, which is.
- Read errors from the exit code and, under `json` and `jsonl`, from the JSON error
  envelope on the last line of stderr. See `coroner help exit-codes`.
