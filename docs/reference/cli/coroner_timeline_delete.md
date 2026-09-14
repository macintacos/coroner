---
title: coroner timeline delete
---

## coroner timeline delete

Delete a timeline and everything in it

### Synopsis

Delete a timeline with its events, tags, annotations, attachments and links, and the blobs
no other attachment uses. This cannot be undone, so it needs --yes; --dry-run shows what
would be deleted instead. Deleting the current timeline leaves none current, and the
overlays that read the timeline keep their other timelines. Under --json it prints the
timeline it deletes, or would.

```
coroner timeline delete <name|id> {--yes | --dry-run} [flags]
```

### Examples

```
  $ coroner timeline delete mongo-outage-2026-09-11 --dry-run
  $ coroner timeline delete mongo-outage-2026-09-11 --yes
```

### Options

```
      --dry-run   Show what would be deleted, and delete nothing
      --yes       Delete without asking
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

