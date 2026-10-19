## Checklist

Rule IDs are defined in `docs/contributing/command-checklist.md`.

- [ ] Checked against [clig.dev](https://clig.dev/); checklist IDs consulted: ___

## Contract changes

A diff under `internal/cli/testdata/script/contract/` is a contract change, except one that only rewords `--help` in `help.txtar` or only renumbers IDs; removing or renaming a command or flag is still one. [STABILITY.md](https://github.com/macintacos/coroner/blob/main/STABILITY.md#classifying-a-contract-change) says whether it is breaking, additive or experimental.

- [ ] No contract change, or `stability:` ___ (`breaking`, `additive` or `experimental`)
