---
title: coroner event edit
---

## coroner event edit

Change an event's text, time, source, tags or other fields

### Synopsis

Change the fields of an event. --at and --ended are read as 'coroner event add' reads them,
and the times they resolve to are printed on stderr. Tags are removed before they are
added; adding a tag it has, or removing one it lacks, changes nothing. Pass none to
--source, --url, --author or --ended to clear it. Pass --text - to read the text from
stdin, which is never read otherwise.

An event is named by its seq in the current timeline (12), by a timeline and seq
(mongo-outage#12), or by its id or the start of it, in any timeline. Seqs and id starts
are for people; scripts pass full ids.

```
coroner event edit <event> [flags]
```

### Examples

```
  $ coroner event edit 12 --add-tag root-cause --remove-tag election --dry-run
  $ coroner event edit mongo-outage-2026-09-11#12 --at "2026-09-11T03:14:07.412Z" --source none
```

### Options

```
      --add-tag tag      Add a tag; repeat for more
      --approximate      Mark the time as a guess; --approximate=false unmarks it
      --at time          When it happened, as a time
      --author name      Who added it, as a name, or none
      --dry-run          Show the edited event, and change nothing
      --ended time       When it ended, as a time, or none
      --remove-tag tag   Remove a tag; repeat for more
      --source source    System, service or person it came from, as a source, or none
      --text text        New text, or - to read it from stdin
      --url url          Permalink to the log line or message, as a url, or none
```

### Options inherited from parent commands

```
      --config path        Config file path (env: CORONER_CONFIG)
      --db path            Database file path (env: CORONER_DB)
      --debug              Print debug detail on stderr (env: CORONER_DEBUG)
      --format format      Output format: {text|json|jsonl}, and more on timeline export (default "text")
  -h, --help               Show help for a command
      --json               Short for --format json
      --no-color           Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input           Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet              Print only essential output
  -t, --timeline name|id   Timeline name|id to act on
      --tz zone            Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner event](coroner_event.md)	 - Add, list, view, edit and delete events

