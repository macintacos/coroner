---
title: coroner timeline view
---

## coroner timeline view

Show a timeline and its events

### Synopsis

Show a timeline and its events in the order they happened. Without a name, id or -t,
show the timeline CORONER_TIMELINE or 'coroner timeline use' selects.

On a terminal it opens an interactive viewer, where ? lists the keys and q quits.
--format text, or piping the output, prints plain text instead: a summary, then the
events, each with the gap since the event before it. --source, --tag and --contains keep
the events 'coroner event list' keeps with the same flags; --json prints the timeline
object alone.

```
coroner timeline view [<name|id>] [flags]
```

### Examples

```
  $ coroner timeline view mongo-outage-2026-09-11
  $ coroner timeline view --tag election --source mongod
  $ coroner timeline view --json
```

### Options

```
      --contains text      Keep events whose text contains this text, ignoring case
      --source source      Keep events from this source; repeat to match any of several
      --tag tag            Keep events with this tag; repeat to require all of them
  -t, --timeline name|id   Timeline name|id to act on
```

### Options inherited from parent commands

```
      --config path     Config file path (env: CORONER_CONFIG)
      --db path         Database file path (env: CORONER_DB)
      --debug           Print debug detail on stderr (env: CORONER_DEBUG)
      --format format   Output format: {text|json|jsonl} (default "text")
  -h, --help            Show help for a command
      --json            Short for --format json
      --no-color        Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input        Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner timeline](coroner_timeline.md)	 - Create, list and switch between timelines

