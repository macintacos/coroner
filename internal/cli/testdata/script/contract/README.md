# Contract scripts

*Audience: anyone, human or agent, adding or changing a testscript under this directory.*

Scripts here pin coroner's machine-facing contract: `--format json` and `jsonl` shapes,
exit codes, and `--help` output. A diff in this directory is a contract change: it needs a
changelog entry and, after 1.0, the stability process. Scripts outside `contract/` can be
refreshed freely with `go test ./internal/cli -run TestScripts -update`.
