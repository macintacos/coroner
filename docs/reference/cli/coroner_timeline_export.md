---
title: coroner timeline export
---

## coroner timeline export

Export a timeline as JSON or JSONL

### Synopsis

Export the timeline -t, CORONER_TIMELINE or 'coroner timeline use' selects, with its
events, their annotations and the sources they name. --format json writes one document,
indented on a terminal; --format jsonl writes a header record, then one record per line.

The filter flags mean what they do on event list, and annotations on the events they
leave out are left out too. --order picks which end --limit keeps events from; exported
events are always in the order they happened. Redirect stdout to write a file.

```
coroner timeline export --format {json | jsonl} [flags]
```

### Examples

```
  $ coroner timeline export -t mongo-outage --format json > rca.json
  $ coroner timeline export --format jsonl --since "2026-09-11T03:00Z" --source mongod > mongod.jsonl
```

### Options

```
      --contains text      Keep events whose text contains this text, ignoring case
      --limit number       Export at most this number of events (default: every one)
      --order order        Keep the earliest or latest events when --limit cuts the export, as an order: {asc|desc} (default "asc")
      --since time         Keep events at or after this time
      --source source      Keep events from this source; repeat to match any of several
      --tag tag            Keep events with this tag; repeat to require all of them
  -t, --timeline name|id   Timeline name|id to act on
      --until time         Keep events before this time
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

