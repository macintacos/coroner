---
title: coroner event add
---

## coroner event add

Add an event to the current timeline

### Synopsis

Add an event to the current timeline, and print the time it resolved to.

A time with no offset is read in --tz, then the config file's default_timezone, then the
local zone. RFC 3339 is stable and natural language experimental, so scripts send RFC 3339.
Run 'coroner help time' for the times you can type.

Pass - to read the text from stdin, which is never read otherwise.

Each --attach copies a file, such as a screenshot, into the blob directory beside the
database, and prints its id, sha256, size and type. Every file is read before anything is
added, so one that cannot be read adds nothing.

--id with --at, and --author when USER may differ, gives the event an id and time of your
own, so running the same command again adds nothing and exits 0, while the same id with
different content exits 4.

--from-file adds a JSONL file of event records, as 'coroner timeline export --format jsonl'
writes them, in one transaction, to the timeline -t, CORONER_TIMELINE or 'coroner timeline
use' selects, which every record's timeline_id must name. Every record is checked first, so
one bad record adds nothing. A record carries its own id and RFC 3339 times, so running the
same file again adds nothing.

On a terminal, with no text or with --interactive, a form asks for each flag, previews the
time as you type it, and prints the command each entry equals. Under --json only --interactive
opens it, and --no-input never does.

```
coroner event add {<text> | - | --from-file {<file> | -}} [flags]
```

### Examples

```
  $ coroner event add "Primary stepped down: replSetStepDown" --at 2026-09-11T03:14:07.412Z \
      --source mongod --tag election
  $ coroner event add "Dashboard at peak" --at 14:02 --attach ./p99.png --attach ./cpu.png
  $ pbpaste | coroner event add - --source slack --at "yesterday 23:02"
  $ coroner event add "Deploy started" --at "around 2026-09-11 14:00" \
      --ended "2026-09-11 14:20" --dry-run --json
  $ coroner event add "Election timeout on db-3" --id 01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2 \
      --at 2026-09-11T03:14:30Z --json
  $ coroner event add -t mongo-outage --from-file events.jsonl --dry-run --json
  $ coroner event add --interactive --source mongod
```

### Options

```
      --approximate      Mark the time as a guess, as a ~ or around prefix does
      --at time          When it happened, as a time (default: now)
      --attach file      Copy this file in as an attachment; repeat for more
      --author name      Who is adding it, as a name (default: config author, then USER)
      --dry-run          Show the event as it would be added, and add nothing
      --ended time       When it ended, as a time, for an event that lasted
      --from-file file   Add every event record in this JSONL file, or - to read them from stdin, in one transaction
      --id uuid          The event's id, as a lowercase UUIDv7 uuid
      --interactive      Open a form to fill in, prefilled from the other flags (needs a terminal)
      --source source    System, service or person it came from, as a source, created on first use
      --tag tag          Keyword such as phase:detect, as a tag; repeat for more
      --url url          Permalink to the log line or message, as a url
```

### Options inherited from parent commands

```
      --config path        Config file path (env: CORONER_CONFIG)
      --db path            Database file path (env: CORONER_DB)
      --debug              Print debug detail on stderr (env: CORONER_DEBUG)
      --format format      Output format: {text | json | jsonl}, and more on timeline export (default "text")
  -h, --help               Show help for a command
      --json               Short for --format json
      --no-color           Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input           Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet              Print only essential output
  -t, --timeline name|id   Timeline name|id to act on
      --tz zone            Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner event](coroner_event.md)	 - Add, list, view, edit, delete and link events

