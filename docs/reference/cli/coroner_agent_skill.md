---
title: coroner agent skill
---

## coroner agent skill

Print the Agent Skill for this version of coroner

### Synopsis

Print the SKILL.md of the coroner Agent Skill this binary was built with, so the skill an
agent reads matches the commands it runs. Its references ship with the skill's directory
in the repository, not in the binary.

```
coroner agent skill [flags]
```

### Examples

```
  $ coroner agent skill
  $ coroner agent skill | diff - ~/.claude/skills/coroner/SKILL.md
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

* [coroner agent](coroner_agent.md)	 - Print what coding agents need to drive coroner

