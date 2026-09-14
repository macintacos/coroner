---
title: coroner overlay create
---

## coroner overlay create

Save a set of timelines under a name

### Synopsis

Save the timelines -t names, in the order given, as an overlay called <name>: at most 64
lowercase letters, digits, '.', '_' or '-', starting with a letter or digit. --overlay <name>
then selects those timelines on event list and timeline export. Deleting a timeline removes it
from its overlays. --dry-run shows the overlay and saves nothing.

```
coroner overlay create <name> -t <timeline>... [flags]
```

### Examples

```
  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency
  $ coroner overlay create checkout-incident -t mongo-failover -t checkout-latency --if-not-exists --json
```

### Options

```
      --dry-run            Show the overlay as it would be saved, and save nothing
      --if-not-exists      Print the existing overlay instead of failing when the name is taken
  -t, --timeline name|id   Timeline name|id the overlay reads; repeat for each, in order
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

* [coroner overlay](coroner_overlay.md)	 - Save sets of timelines to read together

