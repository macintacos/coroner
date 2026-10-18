# Contract scripts

*Audience: anyone, human or agent, adding or changing a testscript under this directory.*

Scripts here pin coroner's machine-facing contract: `--format json` and `jsonl` shapes,
exit codes, and `--help` output. Changing one is a contract change: follow
[AGENTS.md Rule 3](../../../../../AGENTS.md#rules), and `STABILITY.md` (not in the repo
yet — EXC-1303) once it lands. A script whose header marks its fields experimental pins
their shape so a change to them is deliberate, but before 1.0 changing them needs neither
the maintainer's approval nor a major bump.

Assert an exact exit code with `exits <code> coroner [args...]`, which runs like `exec`
and keeps stdout and stderr for the assertions that follow; `! exec` only proves the
command failed.

`go test ./internal/cli -run 'TestScripts/script$' -update` refreshes only the scripts
outside `contract/`. `go test ./internal/cli -run 'TestScripts/contract$' -update`
rewrites the expectations here; review `git diff -- internal/cli/testdata/script/contract`
before committing.
