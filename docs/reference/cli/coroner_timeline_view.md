---
title: coroner timeline view
---

## coroner timeline view

Show a timeline and its events

### Synopsis

Show a timeline: a summary, then its events in the order they happened, each with the
time since the event before it. Without a name or id, show the current timeline.

```
coroner timeline view [<name|id>] [flags]
```

### Examples

```
  $ coroner timeline view mongo-outage-2026-09-11
  $ coroner timeline view --json
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
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner timeline](coroner_timeline.md)	 - Create, list and switch between timelines

