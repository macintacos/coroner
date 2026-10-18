---
title: coroner timeline edit
---

## coroner timeline edit

Rename a timeline or change its title and refs

### Synopsis

Change a timeline's name, title or refs. Its id never changes, so its events, and
anything naming it by id, still resolve after a rename. Refs are removed before they are
added; adding a ref it has, or removing one it lacks, changes nothing.

On a terminal, without --name, --title, --add-ref or --remove-ref, or with --interactive, a
form opens on the timeline, or on the current one when none is named, applies only what you
change, and prints the command it equals. Under --json only --interactive opens it, and
--no-input never does.

```
coroner timeline edit <name|id> [flags]
```

### Examples

```
  $ coroner timeline edit mongo-outage-2026-09-11 --name mongo-failover --add-ref INC-4822
  $ coroner timeline edit mongo-failover --title "" --dry-run --json
  $ coroner timeline edit mongo-failover --interactive
```

### Options

```
      --add-ref stringArray      Add a ticket key or URL; repeat for more
      --dry-run                  Show the edited timeline, and change nothing
      --interactive              Open a form to fill in, prefilled from the other flags (needs a terminal)
      --name name                New name
      --remove-ref stringArray   Remove a ref; repeat for more
      --title title              New title; an empty one clears it
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

