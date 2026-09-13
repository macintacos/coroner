# Output formats and what scripts can rely on

*Audience: anyone reading coroner's output from a script or an agent.*

## Choosing a format

`--format text|json|jsonl` picks the format, and `--json` is short for `--format json`.
Text is the default, and it stays the default when output is piped: coroner never switches
to JSON on its own. A command exits 2 when asked for a format it cannot print.

- `text` is for people.
- `json` is one JSON document, indented when stdout is a terminal and compact otherwise.
- `jsonl` is one JSON object per line.

## What JSON output promises

- Keys are snake_case, and every key is always present: an empty value is `null`, never a
  missing key.
- A collection is wrapped in an object that says whether it was cut short (`truncated`)
  and how many items matched (`total`).
- Instants are fixed-width RFC 3339 in UTC, such as `2026-09-12T14:05:03.000000000Z`. See
  `coroner help time`.
- Each JSONL line is an object whose `type` key says what it holds.

## What consumers must do

- Ignore keys you don't know: new ones are added over time.
- Tolerate enum values you don't know, such as a new `type` or error `kind`.
- Never parse text output or the layout of exported Markdown. Neither is a contract.
- Read errors from the exit code and, under `json` and `jsonl`, from the JSON error
  envelope on the last line of stderr. See `coroner help exit-codes`.
