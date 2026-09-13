---
title: coroner tag list
---

## coroner tag list

List tags with how often and when they were used

### Synopsis

List every tag in the database by name. Each row is the tag, the events carrying it, and
when the first and last of them happened. Piped, the fields are tab-separated.

```
coroner tag list [flags]
```

### Examples

```
  $ coroner tag list
  $ coroner tag list --json
```

### Options inherited from parent commands

```
      --config path     Config file path (env: CORONER_CONFIG)
      --db path         Database file path (env: CORONER_DB)
      --debug           Print debug detail on stderr (env: CORONER_DEBUG)
      --format format   Output format: {text|json|jsonl} (default "text")
  -h, --help            Show help for a command
      --json            Short for --format json
      --no-color        Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input        Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner tag](coroner_tag.md)	 - List the tags events carry

