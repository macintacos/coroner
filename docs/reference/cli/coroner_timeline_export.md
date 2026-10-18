---
title: coroner timeline export
---

## coroner timeline export

Export a timeline as JSON, JSONL, Markdown, Mermaid, HTML or SVG

### Synopsis

Export the timeline -t, CORONER_TIMELINE or 'coroner timeline use' selects, with its
events, their annotations, the sources they name and their attachments. --format json
writes one document, indented on a terminal; --format jsonl writes a header record, then
one record per line; --format md writes a CommonMark outline for people, with times in
UTC unless --tz names a zone.

--format mermaid writes a Mermaid gantt diagram of the events, one section per source, in
the same zone as md; --diagram appends that diagram to --format md.

--format html writes one self-contained page that shows the events on a timeline in a
browser, in the same zone as md, and needs -o.

--format svg writes a static SVG picture of the events, one lane per source, in the same
zone as md, for documents and slides that draw neither Mermaid nor HTML.

The filter flags mean what they do on event list, and annotations on the events they
leave out are left out too. --order picks which end --limit keeps events from; exported
events are always in the order they happened.

-o writes a file instead of stdout. For md and jsonl, -o may instead name a directory, one
that exists or a path ending in /: coroner writes <timeline>.md or <timeline>.jsonl into
it and each attachment's file under attachments/ beside it, named by its sha256, so the
Markdown's images show wherever the directory is committed or unzipped. Otherwise md links
attachments to the files beside the database, which only this machine has, and says so on
stderr, and a jsonl export's attachment records carry metadata and sha256 only, so their
files stay on this machine. Pasting Markdown into GitHub, Linear or Slack uploads none of
its images.

Repeat -t to overlay several timelines in one json, jsonl, md or mermaid export: it holds
every timeline, and their events merge by occurred at, then timeline id, then seq. md labels
each event with its timeline, and mermaid draws a section per timeline and source, as in
mongo-failover/mongod. html and svg export one timeline. An overlay written into a directory
is named after its timelines joined by +, as in mongo-failover+checkout-latency.jsonl.
--overlay <name> exports the timelines an overlay saved with 'coroner overlay create' holds,
in place of -t. json and jsonl carry every link between two exported events.

--group-by is experimental and may change before 1.0.

```
coroner timeline export --format {json | jsonl | md | mermaid | html | svg} [-o {<file> | <directory>/}] [flags]
```

### Examples

```
  $ coroner timeline export -t mongo-outage --format json > rca.json
  $ coroner timeline export --format jsonl --since "2026-09-11T03:00Z" --source mongod > mongod.jsonl
  $ coroner timeline export --format md --tz America/New_York > rca.md
  $ coroner timeline export --format mermaid --source mongod > timeline.mmd
  $ coroner timeline export --format md --diagram > rca.md
  $ coroner timeline export -t inc-4821 --format md -o rca/
  $ coroner timeline export --format html -o inc-4821.html
  $ coroner timeline export -t inc-4821 --format svg > inc-4821.svg
  $ coroner timeline export -t mongo-failover -t checkout-latency --format jsonl > overlay.jsonl
  $ coroner timeline export --overlay checkout-incident --format md -o rca/
```

### Options

```
      --contains text      Keep events whose text contains this text, ignoring case
      --diagram            Append a Mermaid gantt diagram of the events (--format md only)
      --group-by field     Section Markdown events by a field: {source|tag} (experimental)
      --include-raw        Add each event's time as it was given (--format md only)
      --limit number       Export at most this number of events (default: every one)
      --order order        Keep the earliest or latest events when --limit cuts the export, as an order: {asc|desc} (default "asc")
  -o, --output path        Write the export to this path, to stdout when it is -, or, for md and jsonl, into a directory with its attachments (default "-")
      --overlay name       Read the timelines of the overlay called name, instead of -t
      --since time         Keep events at or after this time
      --source source      Keep events from this source; repeat to match any of several
      --tag tag            Keep events with this tag; repeat to require all of them
  -t, --timeline name|id   Timeline name|id to export; repeat to overlay several (json, jsonl, md and mermaid only)
      --until time         Keep events before this time
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

