---
title: coroner timeline
---

## coroner timeline

Create, list and switch between timelines

### Synopsis

A timeline is the named chronology of one investigation. Create one per incident or
ticket, then use it so later commands know where events go.

```
coroner timeline [flags]
```

### Examples

```
  $ coroner timeline create mongo-outage-2026-09-11 --ref INC-4821
  $ coroner timeline use mongo-outage-2026-09-11
  $ coroner timeline list
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

* [coroner](coroner.md)	 - Build root-cause investigation timelines
* [coroner timeline create](coroner_timeline_create.md)	 - Create a timeline
* [coroner timeline list](coroner_timeline_list.md)	 - List timelines, marking the current one
* [coroner timeline use](coroner_timeline_use.md)	 - Set the current timeline

