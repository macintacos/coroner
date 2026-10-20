---
title: coroner agent
---

## coroner agent

Print what coding agents need to drive coroner

### Synopsis

coroner ships an Agent Skill that teaches coding agents to build timelines with flags and
JSON. 'coroner agent skill' prints the one this binary was built with.

```
coroner agent [flags]
```

### Examples

```
  $ coroner agent skill
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

* [coroner](coroner.md)	 - Build root-cause investigation timelines
* [coroner agent skill](coroner_agent_skill.md)	 - Print the Agent Skill for this version of coroner

