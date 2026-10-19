---
title: coroner timeline list
---

## coroner timeline list

List timelines, marking the current one

### Synopsis

List every timeline, oldest first. A '*' marks the current timeline, the one
'coroner timeline use' selected.

On a terminal it opens a picker that also shows each timeline's refs, event count and
last event: / filters, enter makes the timeline under the cursor current, and q quits.
--format text, --json, --no-input, CORONER_NO_INPUT or piping the output prints the list
instead: a marker, the name and the title on each row, tab-separated when piped.

```
coroner timeline list [flags]
```

### Examples

```
  $ coroner timeline list
  $ coroner timeline list --json
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

* [coroner timeline](coroner_timeline.md)	 - Create, list and switch between timelines

