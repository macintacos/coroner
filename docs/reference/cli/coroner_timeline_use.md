---
title: coroner timeline use
---

## coroner timeline use

Set the current timeline

### Synopsis

Make a timeline current, so commands that act on a timeline use it when neither -t nor
CORONER_TIMELINE names one. Each database keeps its own current timeline. --clear leaves
none current. Under --json it prints the timeline it makes current, or null with --clear.

```
coroner timeline use {<name|id> | --clear} [flags]
```

### Examples

```
  $ coroner timeline use mongo-outage-2026-09-11
  $ coroner timeline use --clear
```

### Options

```
      --clear     Leave no timeline current
      --dry-run   Show the timeline that would become current, and change nothing
```

### Options inherited from parent commands

```
      --config path     Config file path (env: CORONER_CONFIG)
      --db path         Database file path (env: CORONER_DB)
      --debug           Print debug detail on stderr (env: CORONER_DEBUG)
      --format format   Output format: {text|json|jsonl}, and more on timeline export (default "text")
  -h, --help            Show help for a command
      --json            Short for --format json
      --no-color        Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input        Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner timeline](coroner_timeline.md)	 - Create, list and switch between timelines

