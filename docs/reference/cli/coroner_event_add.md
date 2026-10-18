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

```
coroner event add {<text> | -} [flags]
```

### Examples

```
  $ coroner event add "Primary stepped down: replSetStepDown" --at 2026-09-11T03:14:07.412Z \
      --source mongod --tag election
  $ pbpaste | coroner event add - --source slack --at "yesterday 23:02"
  $ coroner event add "Deploy started" --at "around 2026-09-11 14:00" \
      --ended "2026-09-11 14:20" --dry-run --json
```

### Options

```
      --approximate     Mark the time as a guess, as a ~ or around prefix does
      --at time         When it happened, as a time (default: now)
      --author name     Who is adding it, as a name (default: config author, then USER)
      --dry-run         Show the event as it would be added, and add nothing
      --ended time      When it ended, as a time, for an event that lasted
      --source source   System, service or person it came from, as a source, created on first use
      --tag tag         Keyword such as phase:detect, as a tag; repeat for more
      --url url         Permalink to the log line or message, as a url
```

### Options inherited from parent commands

```
      --config path        Config file path (env: CORONER_CONFIG)
      --db path            Database file path (env: CORONER_DB)
      --debug              Print debug detail on stderr (env: CORONER_DEBUG)
      --format format      Output format: {text|json|jsonl} (default "text")
  -h, --help               Show help for a command
      --json               Short for --format json
      --no-color           Disable color (env: NO_COLOR, CORONER_NO_COLOR)
  -q, --quiet              Print only essential output
  -t, --timeline name|id   Timeline name|id to act on
      --tz zone            Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner event](coroner_event.md)	 - Add, list, view, edit and delete events

