---
title: coroner timeline list
---

## coroner timeline list

List timelines, marking the current one

### Synopsis

List every timeline, oldest first. A '*' marks the current timeline, the one
'coroner timeline use' selected. Each row is the marker and name, a tab, then the title.

```
coroner timeline list [flags]
```

### Examples

```
  $ coroner timeline list
  $ coroner timeline list --json
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

