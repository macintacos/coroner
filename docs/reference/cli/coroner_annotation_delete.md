---
title: coroner annotation delete
---

## coroner annotation delete

Delete annotations

### Synopsis

Delete annotations by full id or unique id prefix, in any timeline, with their attachments
and the blobs no other attachment uses. Every id is checked before anything is deleted, so
an id that matches nothing deletes none. --dry-run shows what would be deleted instead.

```
coroner annotation delete <id>... [flags]
```

### Examples

```
  $ coroner annotation delete 0199a1b2-7c1e --dry-run
  $ coroner annotation delete 0199a1b2-7c1e 0199a1b2-7d40 --json
```

### Options

```
      --dry-run   Show what would be deleted, and delete nothing
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

* [coroner annotation](coroner_annotation.md)	 - Attach evidence and comments to events and timelines

