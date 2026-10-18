---
title: coroner timeline create
---

## coroner timeline create

Create a timeline

### Synopsis

Create a timeline. Its name is how commands select it: at most 64 lowercase letters,
digits, '.', '_' or '-', starting with a letter or digit. Put free text in --title.
Creating a timeline does not make it current; run 'coroner timeline use' for that.

```
coroner timeline create <name> [flags]
```

### Examples

```
  $ coroner timeline create mongo-outage-2026-09-11 --title "Primary election storm" \
      --ref INC-4821 --ref https://jira.example.com/browse/INC-4821
  $ coroner timeline create mongo-outage-2026-09-11 --if-not-exists --json
```

### Options

```
      --if-not-exists     Print the existing timeline instead of failing when the name is taken
      --ref stringArray   Ticket key or URL the timeline belongs to; repeat for more
      --title title       Free-text title
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

* [coroner timeline](coroner_timeline.md)	 - Create, list and switch between timelines

