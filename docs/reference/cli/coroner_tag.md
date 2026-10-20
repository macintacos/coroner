---
title: coroner tag
---

## coroner tag

List the tags events carry

### Synopsis

A tag is a free lowercase keyword on an event, such as phase:detect. Tags exist only on
the events that carry them, across the whole database.

```
coroner tag [flags]
```

### Examples

```
  $ coroner tag list
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
      --tz zone         Time zone for this invocation (default: config default_timezone, then UTC)
```

### SEE ALSO

* [coroner](coroner.md)	 - Build root-cause investigation timelines
* [coroner tag list](coroner_tag_list.md)	 - List tags with how often and when they were used

