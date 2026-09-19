# Rules for changing coroner

These rules apply to anyone changing coroner, human or agent. This file holds rules and
where to find things, not tutorials.

## Where things live and what to call them

- Commands: `internal/cli` (today `cmd/`). Store: `internal/store` (today `internal/db/`).
  Domain types: `internal/timeline` (not in the repo yet).
- Vocabulary: use the glossary in `CONTEXT.md` (not in the repo yet) for names in code,
  flags, JSON and docs. The noun is `annotation`, not `note`.
- Layering and function-shape rules live in lint ([`.golangci.yml`](.golangci.yml)), not
  here. Layering checks are not in the repo yet.
- Tasks: `.mise/tasks/` (`mise tasks` lists them).

## Rules

1. **clig.dev is mandatory:** check every new or changed command, flag, output or error
   message against [clig.dev](https://clig.dev/) using
   `docs/contributing/command-checklist.md` (not in the repo yet). Cite its rule IDs, as
   in "violates OUT-3".
2. **Never hand-edit generated files:** `internal/store/sqlcgen/**` (today
   `internal/db/db.go`, `internal/db/models.go` and `internal/db/*.sql.go`) is regenerated
   by `mise run format`, whose hk step runs `sqlc generate`. Each issue that adds a
   generated artifact, such as the CLI reference or `schema/*.json`, adds its path and how
   to regenerate it here.
3. **JSON output is a contract:** change it by editing the Go types, regenerating the
   schema and adding a fixture; an additive change bumps `format_version` minor, and any
   other change needs the maintainer's explicit approval and a major bump. Frozen export
   fixtures (`testdata/export/v*/`) are never edited; human text and Markdown layout are
   not contracts.
4. **User-visible behaviour changes docs in the same PR:** update the relevant how-to or
   reference page, and the stability document when a contract is affected.
5. **Commit messages are
   [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):** use `feat!:`
   or `BREAKING CHANGE:` only with the maintainer's approval. Before 1.0, breaks are
   listed under a **Breaking** changelog heading.
6. **Keep the agent skill in sync:** when a command used in `skills/coroner/SKILL.md` (not
   in the repo yet) changes, update the skill in the same PR, or its examples test fails.
7. **Run `mise run preflight` before declaring work done:** it runs lint, test and build
   ([`.mise/tasks/preflight`](.mise/tasks/preflight)).
