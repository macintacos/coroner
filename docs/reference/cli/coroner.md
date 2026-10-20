---
title: coroner
---

## coroner

Build root-cause investigation timelines

### Synopsis

coroner records what happened during an investigation, in order: name a timeline,
add timestamped events from logs, chat and people, then view and export it.

```
coroner <command> [flags]
```

### Examples

```
  $ coroner timeline create checkout-outage --title "Checkout outage"
  $ coroner event add -t checkout-outage "Error rate crossed 5%" --at 2026-09-11T03:14Z --source datadog
  $ coroner event list -t checkout-outage
```

### Options

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
      --version         Show coroner's version
```

### SEE ALSO

* [coroner agent](coroner_agent.md)	 - Print what coding agents need to drive coroner
* [coroner annotation](coroner_annotation.md)	 - Attach evidence and comments to events and timelines
* [coroner attachment](coroner_attachment.md)	 - List, delete and export files attached to events and annotations
* [coroner completion](coroner_completion.md)	 - Generate the autocompletion script for the specified shell
* [coroner event](coroner_event.md)	 - Add, list, view, edit, delete and link events
* [coroner import](coroner_import.md)	 - Import records from a JSONL export
* [coroner overlay](coroner_overlay.md)	 - Save sets of timelines to read together
* [coroner source](coroner_source.md)	 - List sources and fix their names
* [coroner status](coroner_status.md)	 - Show the database and the timeline commands act on
* [coroner tag](coroner_tag.md)	 - List the tags events carry
* [coroner timeline](coroner_timeline.md)	 - Create, list and switch between timelines
* [coroner version](coroner_version.md)	 - Show coroner's version

