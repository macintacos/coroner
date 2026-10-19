---
title: coroner attachment
---

## coroner attachment

List, delete and export files attached to events and annotations

### Synopsis

An attachment is a file, such as a screenshot, kept with one event or annotation. Attach one
with --attach on 'coroner event add', 'coroner event edit', 'coroner annotation add' or
'coroner annotation edit'. Its bytes are stored once, as a blob named by their sha256, in
the directory beside the database.

```
coroner attachment [flags]
```

### Examples

```
  $ coroner event edit 12 --attach ./grafana-p99.png
  $ coroner attachment list -e 12
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
* [coroner attachment delete](coroner_attachment_delete.md)	 - Delete attachments
* [coroner attachment export](coroner_attachment_export.md)	 - Write an attachment's file back out
* [coroner attachment list](coroner_attachment_list.md)	 - List a timeline's attachments

