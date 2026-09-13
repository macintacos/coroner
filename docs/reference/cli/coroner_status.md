---
title: coroner status
---

## coroner status

Show the database and the current timeline

### Synopsis

Show the database coroner uses, its schema version, and the timeline commands act on
with what selected it: -t, CORONER_TIMELINE or 'coroner timeline use'. status never
creates or migrates the database. Its JSON fields are experimental until 1.0.

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
      --format format   Output format: {text|json|jsonl} (default "text")
  -h, --help            Show help for a command
      --json            Short for --format json
      --no-color        Disable color (env: NO_COLOR, CORONER_NO_COLOR)
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner](coroner.md)	 - Build root-cause investigation timelines

