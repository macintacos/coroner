# Rules for changing coroner

These rules apply to anyone changing coroner, human or agent. This file holds rules and
where to find things, not tutorials.

## Where things live

- Commands: `internal/cli` (today `cmd/`). Store: `internal/store` (today `internal/db/`).
  Domain types: `internal/timeline` (not created yet).
- Vocabulary: use the glossary in `CONTEXT.md` (not written yet) for names in code, flags,
  JSON and docs. The noun is `annotation`, not `note`.
- Layering and function-shape rules are enforced by lint ([.golangci.yml](.golangci.yml))
  and are not restated here.
- Tasks: `mise run format`, `mise run lint`, `mise run test`, `mise run build`,
  `mise run preflight`.

## Rules

1. **clig.dev is mandatory:** check every new or changed command, flag, output or error
   message against [clig.dev](https://clig.dev/) using
   `docs/contributing/command-checklist.md` (not written yet). Cite its rule IDs, as in
   "violates OUT-3".
2. **Never hand-edit generated files:** regenerate them with `mise run format`, whose hk
   step runs `sqlc generate`. Today that means `internal/store/sqlcgen/**` (before the
   restructure, `internal/db/db.go`, `internal/db/models.go` and `internal/db/*.sql.go`),
   and each issue that adds a generated artifact, such as the CLI reference or
   `schema/*.json`, adds its path here.
3. **JSON output is a contract:** change it by editing the Go types, regenerating the
   schema and adding a fixture, and bump `format_version` minor for additive changes.
   Never edit frozen export fixtures (`testdata/export/v*/`); any other change needs the
   maintainer's explicit approval and a major bump, while human text and Markdown layout
   are not contracts.
4. **User-visible behaviour changes docs in the same PR:** update the relevant how-to or
   reference page, and the stability document when a contract is affected.
5. **Commit messages are
   [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):** use `feat!:`
   or `BREAKING CHANGE:` only with the maintainer's approval. Before 1.0, breaks are
   listed under a **Breaking** changelog heading.
6. **Keep the agent skill in sync:** when a command used in `skills/coroner/SKILL.md`
   changes, update the skill in the same PR, or its examples test fails. The skill arrives
   later; the rule applies once it exists.
7. **Run `mise run preflight` before declaring work done:** it runs lint, test and build
   ([.mise/tasks/preflight](.mise/tasks/preflight)).
