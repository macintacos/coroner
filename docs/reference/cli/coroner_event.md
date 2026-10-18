---
title: coroner event
---

## coroner event

Add, list, view, edit and delete events

### Synopsis

An event is something that happened at a moment or over an interval, as a source reported
it. Events go to, and are listed from, the timeline -t names, else CORONER_TIMELINE, else
the one 'coroner timeline use' picked. view, edit and delete also take an event's id, from
any timeline.

```
coroner event [flags]
```

### Examples

```
  $ coroner event add "Primary stepped down" --at 2026-09-11T03:14:07Z --source mongod
  $ coroner event list --tag election
```

### Options

```
  -t, --timeline name|id   Timeline name|id to act on
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
* [coroner event add](coroner_event_add.md)	 - Add an event to the current timeline
* [coroner event delete](coroner_event_delete.md)	 - Delete events and their annotations
* [coroner event edit](coroner_event_edit.md)	 - Change an event's text, time, source, tags or other fields
* [coroner event list](coroner_event_list.md)	 - List a timeline's events, filtered
* [coroner event view](coroner_event_view.md)	 - Show an event and its annotations

