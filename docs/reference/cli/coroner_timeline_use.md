---
title: coroner timeline use
---

## coroner timeline use

Set the current timeline

### Synopsis

Make a timeline current, so commands that act on a timeline use it when none is named.
Each database keeps its own current timeline. --clear leaves none current.

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
      --clear   Leave no timeline current
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

