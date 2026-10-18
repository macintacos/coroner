---
title: coroner event delete
---

## coroner event delete

Delete events and their annotations

### Synopsis

Delete events, with their tags and annotations, and print each as it was, so its fields
can be added again. Every event is found before any is deleted: when one does not exist,
nothing is deleted and delete exits 3. Exit 3 means one of the events does not exist: if
a retry exits 3, the first attempt may already have deleted them. A deleted event's seq
is never given to another event.

An event is named by its seq in the current timeline (12), by a timeline and seq
(mongo-outage#12), or by its id or the start of it, in any timeline. Seqs and id starts
are for people; scripts pass full ids.

```
coroner event delete <event>... [flags]
```

### Examples

```
  $ coroner event delete 12 14 --dry-run
  $ coroner event delete mongo-outage-2026-09-11#12 --json
```

### Options

```
      --dry-run   Show what would be deleted, and delete nothing
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

