---
title: coroner event unlink
---

## coroner event unlink

Remove the link of a kind from one event to another

### Synopsis

Remove the link of --kind from the first event to the second, and print it as it was. A link
that does not exist exits 3. --dry-run shows the link it would remove instead.

An event is named by its seq in the current timeline (12), by a timeline and seq
(mongo-outage#12), or by its id or the start of it, in any timeline. Seqs and id starts
are for people; scripts pass full ids.

```
coroner event unlink <event> <event> --kind {led_to | same_as | copied_from} [flags]
```

### Examples

```
  $ coroner event unlink mongo-failover#12 checkout-latency#3 --kind led_to --dry-run
  $ coroner event unlink mongo-failover#12 checkout-latency#3 --kind led_to --json
```

### Options

```
      --dry-run     Show the link that would be removed, and remove nothing
      --kind kind   Link kind: {led_to | same_as | copied_from}
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

