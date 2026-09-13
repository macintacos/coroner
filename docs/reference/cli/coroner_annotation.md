---
title: coroner annotation
---

## coroner annotation

Attach evidence and comments to events and timelines

### Synopsis

An annotation is evidence, raw material kept verbatim such as a log line, or a comment,
attached to an event or to the whole timeline. It has no time of its own in the
chronology.

```
coroner annotation [flags]
```

### Examples

```
  $ pbpaste | coroner annotation add - -e 12 --kind evidence --source mongod
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

* [coroner](coroner.md)	 - Build root-cause investigation timelines
* [coroner annotation add](coroner_annotation_add.md)	 - Attach evidence or a comment to an event or the timeline
* [coroner annotation delete](coroner_annotation_delete.md)	 - Delete annotations
* [coroner annotation edit](coroner_annotation_edit.md)	 - Change an annotation's body, kind, source or URL
* [coroner annotation list](coroner_annotation_list.md)	 - List a timeline's annotations

