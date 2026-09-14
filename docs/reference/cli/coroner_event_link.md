---
title: coroner event link
---

## coroner event link

Link one event to another, in any timeline

### Synopsis

Link the first event to the second. --kind says how they relate: led_to when the first led
to the second, same_as when both record one moment as two sources saw it, and copied_from
when the first was copied from the second. The events may be in different timelines; an
event cannot link to itself. From one event to another there is at most one link of each
kind, so another exits 4; a link in the opposite direction is a separate link. --dry-run
checks the link and adds nothing. 'coroner event view' shows an event's links, and exports
carry every link between two exported events.

An event is named by its seq in the current timeline (12), by a timeline and seq
(mongo-outage#12), or by its id or the start of it, in any timeline. Seqs and id starts
are for people; scripts pass full ids.

```
coroner event link <from-event> <to-event> --kind {led_to | same_as | copied_from} [flags]
```

### Examples

```
  $ coroner event link mongo-failover#12 checkout-latency#3 --kind led_to
  $ coroner event link 0199a1b2-7c1e 0199a1b2-7d40 --kind same_as --json
```

### Options

```
      --author name   Who is adding it, as a name (default: config author, then USER)
      --dry-run       Show the link as it would be added, and add nothing
      --kind kind     Link kind: {led_to | same_as | copied_from}
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

* [coroner event](coroner_event.md)	 - Add, list, view, edit, delete and link events

