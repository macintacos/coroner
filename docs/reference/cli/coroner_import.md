---
title: coroner import
---

## coroner import

Merge a JSONL export into the database

### Synopsis

Merge a coroner JSONL export, as 'coroner timeline export --format jsonl' writes it, into the
database: its timelines, sources, events, annotations and attachments. - reads the export
from stdin, so concatenated exports import together. Every record is checked before
anything is written, and the import is written in one transaction.

Records match by id, and sources by name. A record stored unchanged is skipped, so importing
a file twice changes nothing. A record stored with different content is a conflict: by
default the import writes nothing and exits 4; --on-conflict skip keeps the stored record,
and --on-conflict replace takes the import's. Authors and created and updated times come
from the export. An imported event keeps its seq unless its timeline has already used it.

An attachment is imported with its file from the attachments/ directory beside the export,
which 'coroner timeline export -o <directory>/' writes, or when the database already has
that file. Otherwise it is skipped and listed.

-t names a stored timeline that every record must belong to. Without -t, the export decides
which timelines it imports into.

```
coroner import {<file> | -} [flags]
```

### Examples

```
  $ coroner import colleague-annotations.jsonl --dry-run --json
  $ coroner import colleague-annotations.jsonl
  $ cat a.jsonl b.jsonl | coroner import -
  $ coroner import inc-4821.jsonl --on-conflict skip
```

### Options

```
      --dry-run              Show what the import would change, and write nothing
      --on-conflict policy   What to do with a record stored with different content, as a policy: {fail|skip|replace} (default "fail")
  -t, --timeline name|id     Timeline name|id to act on
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

