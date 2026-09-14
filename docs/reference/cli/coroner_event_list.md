---
title: coroner event list
---

## coroner event list

List a timeline's events, filtered

### Synopsis

List the current timeline's events in the order they happened: by occurred at, then seq.

Repeat -t to overlay several timelines: their events merge into one list by occurred at,
then timeline, then seq, and each text row starts with its timeline's name. --json lists
the selected timelines under timelines, and --format jsonl starts with a timeline line for
each, so an event's timeline_id names one of them.

--since is inclusive and --until exclusive. Repeated --source flags match any of them,
and repeated --tag flags must all match. Times with no offset are read, and times print,
in --tz, then the config file's default_timezone, then the local zone. Piped, the
fields are tab-separated.

```
coroner event list [flags]
```

### Examples

```
  $ coroner event list --tag election --source mongod
  $ coroner event list --since "2026-09-11T03:00Z" --until "2026-09-11T04:00Z" --tag election --format jsonl
  $ coroner event list --contains stepdown --order desc --limit 20
  $ coroner event list -t mongo-failover -t checkout-latency --since 2026-09-10T14:00Z --json
```

### Options

```
      --contains text   Keep events whose text contains this text, ignoring case
      --limit number    Show at most this number of events (default: every one)
      --order order     Order by occurred at, then seq, as an order: {asc|desc} (default "asc")
      --since time      Keep events at or after this time
      --source source   Keep events from this source; repeat to match any of several
      --tag tag         Keep events with this tag; repeat to require all of them
      --until time      Keep events before this time
```

### Options inherited from parent commands

```
      --config path        Config file path (env: CORONER_CONFIG)
      --db path            Database file path (env: CORONER_DB)
      --debug              Print debug detail on stderr (env: CORONER_DEBUG)
      --format format      Output format: {text|json|jsonl}, and more on timeline export (default "text")
  -h, --help               Show help for a command
      --json               Short for --format json
      --no-color           Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input           Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet              Print only essential output
  -t, --timeline name|id   Timeline name|id to act on
      --tz zone            Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner event](coroner_event.md)	 - Add, list, view, edit and delete events

