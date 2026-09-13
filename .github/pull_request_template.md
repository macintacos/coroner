## Checklist

Rule IDs are defined in `docs/contributing/command-checklist.md`.

- [ ] Checked against [clig.dev](https://clig.dev/); checklist IDs consulted: ___

## Contract changes

A diff under `internal/cli/testdata/script/contract/` is a contract change. Additive adds keys, enum values, commands, flags, exit codes or kinds; breaking removes or renames a key, or changes its type, nullability, timestamp format, filter semantics, default order or an exit code's meaning; experimental changes only what a script's header marks experimental.

- [ ] No contract change, or `stability:` ___ (`breaking`, `additive` or `experimental`)
- [ ] The commit type matches: git-cliff builds the changelog from the history, and a break before 1.0 takes `feat!:` or `BREAKING CHANGE:` only with the maintainer's approval (AGENTS.md Rule 5)
