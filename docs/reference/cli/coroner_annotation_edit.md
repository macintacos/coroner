---
title: coroner annotation edit
---

## coroner annotation edit

Change an annotation's body, kind, source or URL

### Synopsis

Change an annotation's body, kind, source or URL. Its author, event and timeline never
change. <id> is a full id or a unique id prefix, in any timeline. --body - reads the body
from stdin, which loses one trailing newline.

```
coroner annotation edit <id> [flags]
```

### Examples

```
  $ coroner annotation edit 0199a1b2-7c1e --kind comment
  $ coroner annotation edit 0199a1b2-7c1e --body - --dry-run --json < stepdown.log
```

### Options

```
      --body body       New body, or - to read it from stdin
      --dry-run         Show the edited annotation, and change nothing
      --kind kind       New annotation kind: {evidence|comment}
      --source source   New source, created on first use
      --url url         New permalink, as a url
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

* [coroner annotation](coroner_annotation.md)	 - Attach evidence and comments to events

