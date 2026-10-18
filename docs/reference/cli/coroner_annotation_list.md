---
title: coroner annotation list
---

## coroner annotation list

List a timeline's annotations

### Synopsis

List a timeline's annotations: those on the whole timeline first, then those on events in
the order the events happened, oldest first. -e lists one event's. Each row is the id,
kind, author, event and the body's first line; piped, the fields are tab-separated.

Scripts should treat kind as an open set: later versions may add kinds.

```
coroner annotation list [-e <ref>] [flags]
```

### Examples

```
  $ coroner annotation list
  $ coroner annotation list -e 12 --json
```

### Options

```
  -e, --event ref          Only the annotations on this event, as a ref: seq, timeline#seq or id
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

* [coroner annotation](coroner_annotation.md)	 - Attach evidence and comments to events and timelines

