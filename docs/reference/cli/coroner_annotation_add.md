---
title: coroner annotation add
---

## coroner annotation add

Attach evidence or a comment to an event or the timeline

### Synopsis

Attach an annotation to the event -e names, or to the whole timeline without -e. Evidence
is kept verbatim: pass - to read a log line or chat message from stdin, which loses one
trailing newline and is never read otherwise.

-e takes a seq such as 12, a timeline and seq such as mongo-outage#12, or an event id or
unique id prefix. The event must be in the timeline the annotation goes to.

```
coroner annotation add {<body> | -} --kind {evidence|comment} [flags]
```

### Examples

```
  $ coroner annotation add - -e 12 --kind evidence --source mongod < stepdown.log
  $ coroner annotation add "Step-down lands 5 s after the COLLSCAN burst" -e 12 --kind comment
  $ coroner annotation add "Root cause: undersized oplog" --kind comment --dry-run --json
```

### Options

```
      --author name        Who is adding it, as a name (default: config author, then USER)
      --dry-run            Show the annotation as it would be added, and add nothing
  -e, --event ref          Event to attach it to, as a ref: seq, timeline#seq or id (default: the timeline)
      --kind kind          Annotation kind: {evidence|comment}
      --source source      System, service or person it came from, as a source, created on first use
  -t, --timeline name|id   Timeline name|id to act on
      --url url            Permalink to the log line or message, as a url
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

* [coroner annotation](coroner_annotation.md)	 - Attach evidence and comments to events

