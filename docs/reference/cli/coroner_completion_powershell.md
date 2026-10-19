---
title: coroner completion powershell
---

## coroner completion powershell

Generate the autocompletion script for powershell

### Synopsis

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	coroner completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.


```
coroner completion powershell [flags]
```

### Examples

```
  PS> coroner completion powershell | Out-String | Invoke-Expression
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

