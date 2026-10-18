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
  $ coroner version --json
```

### Options

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
      --version         Show coroner's version
```

### SEE ALSO

* [coroner completion](coroner_completion.md)	 - Generate the autocompletion script for the specified shell
* [coroner status](coroner_status.md)	 - Show the database and the timeline commands act on
* [coroner timeline](coroner_timeline.md)	 - Create, list and switch between timelines
* [coroner version](coroner_version.md)	 - Show coroner's version

