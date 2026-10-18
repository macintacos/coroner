---
title: coroner source list
---

## coroner source list

List sources with how often and when they were used

### Synopsis

List every source in the database by name. Each row is the name, its events, its
annotations, and when its first and last events happened, or '-' when no event names it.
Piped, the fields are tab-separated.

```
coroner source list [flags]
```

### Examples

```
  $ coroner source list
  $ coroner source list --json
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
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner source](coroner_source.md)	 - List sources and fix their names

