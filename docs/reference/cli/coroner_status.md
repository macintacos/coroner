---
title: coroner status
---

## coroner status

Show the database and the timeline commands act on

### Synopsis

Show the database coroner uses, its schema version, the timeline commands act on, and
whether -t, CORONER_TIMELINE or 'coroner timeline use' selected it. status never
migrates the database or creates it. Its JSON fields are experimental until 1.0.

```
coroner status [flags]
```

### Examples

```
  $ coroner status
  $ CORONER_TIMELINE=mongo-outage-2026-09-11 coroner status --json
```

### Options

```
  -t, --timeline name|id   Timeline name|id to act on
```

### Options inherited from parent commands

```
      --config path     Config file path (env: CORONER_CONFIG)
      --db path         Database file path (env: CORONER_DB)
      --debug           Print debug detail on stderr (env: CORONER_DEBUG)
      --format format   Output format: {text | json | jsonl}, and more on timeline export (default "text")
  -h, --help            Show help for a command
      --json            Short for --format json
      --no-color        Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input        Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner](coroner.md)	 - Build root-cause investigation timelines

