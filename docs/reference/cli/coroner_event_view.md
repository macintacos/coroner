---
title: coroner event view
---

## coroner event view

Show an event and its annotations

### Synopsis

Show every field of an event, then its annotations. --json prints the event object alone.

An event is named by its seq in the current timeline (12), by a timeline and seq
(mongo-outage#12), or by its id or the start of it, in any timeline. Seqs and id starts
are for people; scripts pass full ids.

```
coroner event view <event> [flags]
```

### Examples

```
  $ coroner event view 12
  $ coroner event view mongo-outage-2026-09-11#12 --json
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

