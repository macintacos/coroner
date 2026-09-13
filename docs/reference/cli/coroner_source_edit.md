---
title: coroner source edit
---

## coroner source edit

Rename a source

### Synopsis

Rename a source everywhere it is used. Events and annotations refer to a source by id,
so they all follow the new name. A name another source has is refused: merging sources
is not supported yet.

```
coroner source edit <name> --name <new-name> [flags]
```

### Examples

```
  $ coroner source edit mongo --name mongodb --dry-run
  $ coroner source edit mongo --name db-2/mongod
```

### Options

```
      --dry-run         Show how many events and annotations the rename touches, and change nothing
      --name new-name   Rename it to new-name
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

* [coroner source](coroner_source.md)	 - List sources and fix their names

