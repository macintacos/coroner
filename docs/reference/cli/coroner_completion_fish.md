---
title: coroner completion fish
---

## coroner completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	coroner completion fish | source

To load completions for every new session, execute once:

	coroner completion fish > ~/.config/fish/completions/coroner.fish

You will need to start a new shell for this setup to take effect.


```
coroner completion fish [flags]
```

### Examples

```
  $ coroner completion fish > ~/.config/fish/completions/coroner.fish
```

### Options

```
      --no-descriptions   disable completion descriptions
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
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner completion](coroner_completion.md)	 - Generate the autocompletion script for the specified shell

