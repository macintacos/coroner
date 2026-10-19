---
title: coroner overlay
---

## coroner overlay

Save sets of timelines to read together

### Synopsis

An overlay is several timelines read together as one chronology. Save one under a name with
'coroner overlay create', then pass --overlay <name> to event list and timeline export instead
of repeating -t. An overlay is kept in this database only: exports never carry it.

```
coroner overlay [flags]
```

### Examples

```
  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency
  $ coroner event list --overlay checkout-incident
```

### Options inherited from parent commands

```
      --config path     Config file path (env: CORONER_CONFIG)
      --db path         Database file path (env: CORONER_DB)
      --debug           Print debug detail on stderr (env: CORONER_DEBUG)
      --format format   Output format: {text | json | jsonl}, and more on timeline export (default "text")
  -h, --help            Show help for a command
      --json            Short for --format json
      --no-color        Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input        Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner](coroner.md)	 - Build root-cause investigation timelines
* [coroner overlay create](coroner_overlay_create.md)	 - Save a set of timelines under a name

