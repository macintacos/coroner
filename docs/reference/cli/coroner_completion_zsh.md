---
title: coroner completion zsh
---

## coroner completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(coroner completion zsh)

To load completions for every new session, execute once:

#### Linux:

	coroner completion zsh > "${fpath[1]}/_coroner"

#### macOS:

	coroner completion zsh > $(brew --prefix)/share/zsh/site-functions/_coroner

You will need to start a new shell for this setup to take effect.


```
coroner completion zsh [flags]
```

### Examples

```
  $ coroner completion zsh > "${fpath[1]}/_coroner"
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
      --format format   Output format: {text|json|jsonl} (default "text")
  -h, --help            Show help for a command
      --json            Short for --format json
      --no-color        Disable color (env: NO_COLOR, CORONER_NO_COLOR)
      --no-input        Never open a form, picker or viewer (env: CORONER_NO_INPUT)
  -q, --quiet           Print only essential output
      --tz zone         Time zone for this invocation (default: config default_timezone, then TZ)
```

### SEE ALSO

* [coroner completion](coroner_completion.md)	 - Generate the autocompletion script for the specified shell

