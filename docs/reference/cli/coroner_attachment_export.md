---
title: coroner attachment export
---

## coroner attachment export

Write an attachment's file back out

### Synopsis

Write the file an attachment holds, byte for byte, to the file -o names, or to stdout with
-o -, the default, which refuses a terminal. -o never overwrites a file unless --force is
given. <id> is a full id or a unique id prefix, in any timeline.

```
coroner attachment export <id> [-o <file>] [flags]
```

### Examples

```
  $ coroner attachment export 0199a1b2-7c1e -o grafana-p99.png
  $ coroner attachment export 0199a1b2-7c1e > grafana-p99.png
```

### Options

```
      --force         Overwrite the file -o names when it exists
  -o, --output file   Write the attachment to this file, or to stdout when it is - (default "-")
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

* [coroner attachment](coroner_attachment.md)	 - List, delete and export files attached to events and annotations

