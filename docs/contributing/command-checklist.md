# Command checklist

Every command, flag, output, and error in coroner **must** follow
[clig.dev](https://clig.dev/).

This page distills clig.dev into short rules adapted to coroner and cobra. Cite rules by
ID in reviews and in the pull request checkbox ("violates OUT-3"). IDs are append-only:
never renumber a rule, only retire it.

## Basics

- **B-1** Use cobra for every command and flag. No hand-rolled parsing.
  ([clig.dev](https://clig.dev/#the-basics))
- **B-2** Exit 0 on success and non-zero on failure. Map the important failure modes to
  distinct exit codes. ([clig.dev](https://clig.dev/#the-basics))
- **B-3** Primary output, and anything machine-readable, goes to stdout.
  ([clig.dev](https://clig.dev/#the-basics))
- **B-4** Messages, warnings, progress, and errors go to stderr. Data goes to `app.Out`,
  messages to `app.Err` (the `cli.App` writers). Never `root.SetOut`: cobra prints
  deprecation notices via `OutOrStderr()`, so they would corrupt JSON on stdout. The one
  exception: set around `InitDefaultCompletionCmd`, which captures the writer once, and
  cleared before anything runs. ([clig.dev](https://clig.dev/#the-basics))

## Help

- **H-1** `-h`/`--help` on every command shows full help and ignores other arguments.
  Never overload `-h`. ([clig.dev](https://clig.dev/#help))
- **H-2** `coroner help`, `coroner help <command>`, and `coroner <command> -h` all work.
  ([clig.dev](https://clig.dev/#help))
- **H-3** A command missing required arguments prints concise help: a description, one or
  two examples, and a pointer to `--help`. ([clig.dev](https://clig.dev/#help))
- **H-4** Lead with examples. Every command sets cobra's `Example` field with realistic
  incident data. `internal/cli/examples_test.go` parses each `coroner` line in it against
  the command tree and skips lines with `$(…)` or backticks, so avoid both.
  ([clig.dev](https://clig.dev/#help))
- **H-5** Show the most common commands and flags first, and group root help (`event add`
  and `event list` before `timeline delete`). ([clig.dev](https://clig.dev/#help))
- **H-6** Link to the web docs page for the command, and put a feedback or issues URL in
  root help. ([clig.dev](https://clig.dev/#help))
- **H-7** If input looks like a typo, suggest the fix; never run the correction
  automatically. Keep Levenshtein suggestions on. **Never** set
  `cobra.EnablePrefixMatching`. ([clig.dev](https://clig.dev/#help))
- **H-8** If a command expects stdin and stdin is a TTY, print help or a hint instead of
  hanging. ([clig.dev](https://clig.dev/#help))

## Documentation

- **D-1** Generate the web docs from the same cobra command tree.
  ([clig.dev](https://clig.dev/#documentation))
- **D-2** Ship terminal docs with the binary as help topics (`coroner help <topic>`).
  ([clig.dev](https://clig.dev/#documentation))
- **D-3** Man pages are optional and deferred.
  ([clig.dev](https://clig.dev/#documentation))

## Output

- **OUT-1** Human-readable output is the default. Detect a TTY on each stream separately.
  ([clig.dev](https://clig.dev/#output))
- **OUT-2** Machine-readable output must not hurt usability. Piped text stays text:
  tab-separated, untruncated, no color, one record per line. **No auto-JSON when piped.**
  clispec recommends it; rejected, because `coroner event list | grep` must do what people
  expect. ([clig.dev](https://clig.dev/#output))
- **OUT-3** `--json` gives JSON. The general form is `--format`.
  ([clig.dev](https://clig.dev/#output))
- **OUT-4** Print brief output on success. `-q`/`--quiet` suppresses everything
  non-essential. ([clig.dev](https://clig.dev/#output))
- **OUT-5** When state changes, say what changed, including the timeline it touched.
  ([clig.dev](https://clig.dev/#output))
- **OUT-6** Make current state easy to see: `coroner status` shows the database path,
  schema version, current timeline, and which layer selected it.
  ([clig.dev](https://clig.dev/#output))
- **OUT-7** Suggest the next command where it helps, for example after `timeline create`.
  ([clig.dev](https://clig.dev/#output))
- **OUT-8** Anything that crosses the program's own boundary is explicit, such as writing
  a file. The database is internal state and is exempt.
  ([clig.dev](https://clig.dev/#output))
- **OUT-9** Color is off when the stream isn't a TTY, `NO_COLOR` or `CORONER_NO_COLOR` is
  set and non-empty, `TERM=dumb`, or `--no-color` is passed.
  ([clig.dev](https://clig.dev/#output))
- **OUT-10** No spinners or animation when stderr isn't a TTY.
  ([clig.dev](https://clig.dev/#output))
- **OUT-11** stderr is not a log file: no `WARN`/`ERR` level labels outside verbose or
  debug mode. ([clig.dev](https://clig.dev/#output))
- **OUT-12** Page only when stdout is a TTY, with `less -FIRX`. The pager is deferred: AWS
  CLI v2's default-on pager is the cautionary tale. ([clig.dev](https://clig.dev/#output))

## Errors

- **E-1** Catch expected errors and rewrite them for humans, with the fix.
  ([clig.dev](https://clig.dev/#errors))
- **E-2** Signal over noise: group repeated errors under one header with a count and the
  first few occurrences. ([clig.dev](https://clig.dev/#errors))
- **E-3** Put the most important line last. Use red sparingly.
  ([clig.dev](https://clig.dev/#errors))
- **E-4** Unexpected errors print a short message and an issue URL, with no raw stack
  trace by default. `SilenceUsage` and `SilenceErrors` are set on root, and errors print
  once, from `cli.Run`, which also maps them to exit codes. Debug detail appears only
  under `--debug`/`CORONER_DEBUG`. ([clig.dev](https://clig.dev/#errors))

## Arguments and flags

- **A-1** Prefer flags to positional arguments. A positional is allowed only for the
  object acted on: a name, an ID, or event text.
  ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-2** Every flag has a long form. Short flags only for very common flags (`-t`, `-q`,
  `-h`). ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-3** Never use two positionals that mean different things.
  ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-4** Use standard names: `--all`, `--force`, `--json`, `--dry-run` (long only; `-n`
  means "count" elsewhere), `--yes`, `--no-input`, `--quiet`, `--version`, and
  `-o/--output` means a *file*. Never `-v`: it is ambiguous between verbose and version.
  ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-5** Defaults suit most users. ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-6** Never *require* a prompt. With no TTY, fail and name the flag to pass.
  ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-7** Confirm dangerous actions. Deleting a whole timeline requires `--yes`.
  ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-8** `-` means stdin or stdout. **Never read stdin implicitly**: agent harnesses
  attach a non-TTY stdin that never closes.
  ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-9** Optional-value flags take a keyword such as `none`, never an empty string.
  ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-10** Global flags work before or after the subcommand, so keep them persistent on
  root. ([clig.dev](https://clig.dev/#arguments-and-flags))
- **A-11** Never read secrets from flags or environment variables.
  ([clig.dev](https://clig.dev/#arguments-and-flags))

## Interactivity

- **I-1** Prompts and TUI only on a TTY. The bubbletea forms and viewer make this a live
  rule. ([clig.dev](https://clig.dev/#interactivity))
- **I-2** `--no-input` and `CORONER_NO_INPUT` disable every prompt: forms, the picker and
  the viewer. ([clig.dev](https://clig.dev/#interactivity))
- **I-3** Ctrl-C always works. ([clig.dev](https://clig.dev/#interactivity))

## Subcommands

- **S-1** A flag name means the same thing on every command (`-t`, `--format`, `--since`).
  ([clig.dev](https://clig.dev/#subcommands))
- **S-2** Keep noun-verb levels consistent and reuse the [verb glossary](#verb-glossary).
  ([clig.dev](https://clig.dev/#subcommands))
- **S-3** No similar or ambiguous names: no `update` next to `upgrade`, no `show` next to
  `view`. ([clig.dev](https://clig.dev/#subcommands))

## Robustness

- **R-1** Validate all input before writing anything. Imports check every line before
  inserting. ([clig.dev](https://clig.dev/#robustness-guidelines))
- **R-2** Respond within 100 ms. Show progress for long operations on a TTY only.
  ([clig.dev](https://clig.dev/#robustness-guidelines))
- **R-3** `busy_timeout` 5 s; a busy database exits 5 with a clear message.
  ([clig.dev](https://clig.dev/#robustness-guidelines))
- **R-4** Be recoverable and idempotent where possible: re-running an import with the same
  IDs does not duplicate. ([clig.dev](https://clig.dev/#robustness-guidelines))
- **R-5** Crash-only: one SQLite transaction per command, and no cleanup needed after an
  interrupt. ([clig.dev](https://clig.dev/#robustness-guidelines))
- **R-6** Expect misuse: concurrent invocations, wrapping in scripts, case-insensitive
  filesystems. ([clig.dev](https://clig.dev/#robustness-guidelines))

## Future-proofing

- **F-1** Commands, arguments, flags, config, environment variables, JSON, and exit codes
  are all interfaces. Changes are additive.
  ([clig.dev](https://clig.dev/#future-proofing))
- **F-2** In-program deprecation warnings before any non-additive change, naming the
  migration. The window is **6 months / 1 minor release**, not kubectl's 12 months / 2
  minors. ([clig.dev](https://clig.dev/#future-proofing))
- **F-3** Human output may change. Scripts use `--format json` or `--format jsonl`.
  ([clig.dev](https://clig.dev/#future-proofing))
- **F-4** No catch-all command: `coroner "text"` must not mean `event add`.
  ([clig.dev](https://clig.dev/#future-proofing))
- **F-5** No abbreviations, and no aliases in the MVP (`ls`, `rm`). An alias, if ever
  added, is explicit and permanent. ([clig.dev](https://clig.dev/#future-proofing))
- **F-6** No time bombs: no network calls and no update checks by default.
  ([clig.dev](https://clig.dev/#future-proofing))

## Signals

- **SIG-1** On Ctrl-C, exit immediately, roll back the open transaction, and exit 130.
  ([clig.dev](https://clig.dev/#signals))
- **SIG-2** A second Ctrl-C skips any remaining cleanup.
  ([clig.dev](https://clig.dev/#signals))

## Configuration and environment

- **C-1** Precedence is flags, then environment, then project config, then user config,
  then system config. ([clig.dev](https://clig.dev/#configuration))
- **C-2** XDG on macOS too; never `os.UserConfigDir`, which returns
  `~/Library/Application Support` on macOS. ([clig.dev](https://clig.dev/#configuration))
- **C-3** Environment variable names are upper-case with a `CORONER_` prefix and
  single-line values. ([clig.dev](https://clig.dev/#environment-variables))
- **C-4** Honor the general-purpose variables: `NO_COLOR`, `TERM`, `TZ`, `HOME`, `XDG_*`,
  and `EDITOR` and `PAGER` once an editor or pager exists.
  ([clig.dev](https://clig.dev/#environment-variables))
- **C-5** Never modify config that doesn't belong to coroner.
  ([clig.dev](https://clig.dev/#configuration))

## Naming, distribution, and analytics

- **N-1** Command and flag names are lower-case and dash-separated.
  ([clig.dev](https://clig.dev/#naming))
- **DIST-1** Ship a single binary, with uninstall instructions next to the install
  instructions. ([clig.dev](https://clig.dev/#distribution))
- **AN-1** No telemetry. ([clig.dev](https://clig.dev/#analytics))

## Verb glossary

Every new command reuses one of these verbs.

| Verb | Meaning |
| --- | --- |
| `create` | Make a new container object |
| `add` | Append a member to a container |
| `list` | Many objects, filterable |
| `view` | One object in detail |
| `edit` | Change fields of an existing object; `--add-x`/`--remove-x` for sets |
| `delete` | Remove an object |
| `use` | Set the stored current selection |
| `export` | Render an object into a document format |
| `import` | Ingest the machine format that `list` and `export` produce |
| `link` | Connect one object to another with a directed, typed link |
| `unlink` | Remove a link `link` made |

Rejected synonyms: `show`, `get`, `describe`, and `inspect` (use `view`); `rm` and
`remove` (use `delete`); `update` (use `edit`). S-3 forbids `show` next to `view`, so
"show a timeline" is `timeline view`.

Help text follows
[gh's command-line syntax](https://github.com/cli/cli/blob/trunk/docs/command-line-syntax.md):
`<placeholder>`, `[optional]`, `{a | b}` for a required choice, `...` for a repeatable
argument, and dash-case names.
