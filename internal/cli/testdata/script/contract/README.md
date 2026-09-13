# Contract scripts

*Audience: anyone, human or agent, adding or changing a testscript under this directory.*

Scripts here pin coroner's machine-facing contract: `--format json` and `jsonl` shapes,
exit codes, and `--help` output, every command's in `help.txtar`. Changing one is a
contract change: follow [AGENTS.md Rule 3](../../../../../AGENTS.md#rules), and
`STABILITY.md` (not in the repo yet — EXC-1303) once it lands. `CODEOWNERS` requests the
maintainer's review of any change here. A script whose header marks fields or kinds
experimental pins their shape so a change to them is deliberate, but before 1.0 changing
them needs neither the maintainer's approval nor a major bump.

Assert an exact exit code with `exits <code> coroner [args...]`, which runs like `exec`
and keeps stdout and stderr for the assertions that follow; `! exec` only proves the
command failed.

The clock is pinned by `CORONER_TEST_NOW`, and record IDs are numbered in order from a
counter file named by `CORONER_TEST_IDS`, so a script's output is the same on every run.
The counter is unlocked: never run coroner in the background.

When a success's whole stdout is a document, compare it with `cmp stdout <name>.json`
against a file in the script's own archive. Check a single field with a `stdout` pattern,
preceded by `jsonvalid stdout`, which fails unless stdout is empty or holds only JSON
values. Output that differs by machine, such as a path under `$WORK`, stays a pattern.
Match an error envelope as the last line of stderr by pattern: its `kind` is the contract,
its `message` and `hint` are not.

`go test ./internal/cli -run 'TestScripts/script$' -update` refreshes only the scripts
outside `contract/`. `go test ./internal/cli -run 'TestScripts/contract$' -update`
rewrites the files `cmp` compares against here, never a `cmpenv` file or a pattern; review
`git diff -- internal/cli/testdata/script/contract` before committing.
