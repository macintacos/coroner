# Rules for changing coroner

These rules apply to anyone changing coroner, human or agent. This file holds rules and
where to find things, not tutorials.

coroner is a Go CLI that records root-cause investigation timelines, with their events,
annotations and sources, in one SQLite file. Commands are noun-verb (`coroner event add`),
and exports are the only sharing contract.

## How to work here

Tasks live in [`.mise/tasks/`](.mise/tasks/); `mise tasks` lists them.

- `mise run setup` installs the pinned tools and Go modules and registers the git hooks.
- `mise run build` builds the binary into `bin/`.
- `mise run format` runs every formatter in write mode, plus `go mod tidy`,
  `sqlc generate` and the CLI reference generator.
- `mise run lint` runs the linters without writing, then fails if the CLI reference is
  stale. It does not check YAML, TOML, shell or Pkl formatting, `go mod tidy` or
  `sqlc generate`; only `mise run format` and `pre-commit` apply those.
- `mise run test` runs the Ginkgo suites, and through them the testscript end-to-end
  scripts and the rapid properties; a package pattern narrows it, and `--focus <regexp>`
  narrows Ginkgo specs only, so plain `Test*` functions such as `TestScripts` still run.
- `mise run preflight` runs lint, test and build. It is the gate before declaring work
  done (Rule 7).

The [`hk.pkl`](hk.pkl) hooks run on their own: `pre-commit` runs every formatter and
linter and applies fixes, regenerating the CLI reference when a help topic or the Go code
it documents changes, and `pre-push` runs `mise run test` when Go files, `go.mod` or
`go.sum` change.

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs on every pull request and
push to `main`: `mise run lint`, the CLI reference check, `mise run test`, then
`mise run format` failing on any diff. A commit made with `--no-verify` still fails there.

## Routing

Read the digraph as a checklist, not a single path:
**load every file whose edge matches the change** before editing that area. A box marked
"not in the repo yet" names the issue that will land that file. Until it lands there is
nothing to load: skip the box, and never create its file early.

```graphviz
digraph coroner_rules_router {
    "Working on coroner" [shape=doublecircle];
    "What does the change touch?" [shape=diamond];

    "Load AGENTS.md § Rules" [shape=box];
    "Load docs/contributing/command-checklist.md" [shape=box];
    "Load .golangci.yml" [shape=box];
    "Load CONTEXT.md" [shape=box];
    "Load STABILITY.md (not in the repo yet — EXC-1303)" [shape=box];
    "Load skills/coroner/SKILL.md (not in the repo yet — EXC-1277)" [shape=box];
    "Load docs/agents/documentation-rules.md" [shape=box];
    "Load docs/agents/timeline-rules.md" [shape=box];
    "Load docs/agents/store-rules.md" [shape=box];
    "Load docs/agents/timeparse-rules.md" [shape=box];

    "Working on coroner" -> "What does the change touch?";
    "What does the change touch?" -> "Load AGENTS.md § Rules" [label="any change: the repository rules always apply"];
    "What does the change touch?" -> "Load docs/contributing/command-checklist.md" [label="a command, flag, output or error message (Rule 1)"];
    "What does the change touch?" -> "Load .golangci.yml" [label="Go code: function shape and layering are lint rules"];
    "What does the change touch?" -> "Load CONTEXT.md" [label="naming something in code, flags, JSON or docs"];
    "What does the change touch?" -> "Load STABILITY.md (not in the repo yet — EXC-1303)" [label="JSON output, exports or another contract (Rule 3)"];
    "What does the change touch?" -> "Load skills/coroner/SKILL.md (not in the repo yet — EXC-1277)" [label="a command the agent skill uses (Rule 6)"];
    "What does the change touch?" -> "Load docs/agents/documentation-rules.md" [label="adding, removing, renaming or changing a doc, a mise task or hk hook, a docs/agents/ rule file or a generated artifact; an area's first rules; landing anything marked not in the repo yet; or user-visible behaviour (Rule 4)"];
    "What does the change touch?" -> "Load docs/agents/timeline-rules.md" [label="internal/timeline, or code that stores, orders or validates its types"];
    "What does the change touch?" -> "Load docs/agents/store-rules.md" [label="internal/store: a migration, a query, sqlc.yaml, the SQL it runs, or the store's transactions and row mapping"];
    "What does the change touch?" -> "Load docs/agents/timeparse-rules.md" [label="internal/timeparse, or code that turns typed time input into an instant"];
}
```

## Where things live and what to call them

- Commands: `internal/cli`. Store: `internal/store`. Domain types: `internal/timeline`.
  Typed time input: `internal/timeparse`. Config file, XDG paths and precedence:
  `internal/config`. `main.go` builds `cli.App` from the process; code below it takes
  everything from `cli.App`, never from `os`. Dev tools run with `go run`:
  `internal/tools/` (`docgen` writes the CLI reference, `compatfixture` a release's
  fixture database).
- Help topics: `internal/cli/topics/<topic>.md`, embedded and printed as-is by
  `coroner help <topic>`, and copied into the CLI reference by `docgen`. Specs fail when
  `exit-codes` disagrees with the exit-code map or `environment` misses an environment
  variable the code reads.
- Errors and exit codes: a command's hooks or body return a `*cli.Error` whose `Kind`
  picks the exit code; `exitCodes` in [`internal/cli/errors.go`](internal/cli/errors.go)
  is the table. Any other error, or a panic, exits 1 as `internal`. A command declares the
  output formats it supports in its `formats` annotation, and any other `--format` or
  `--json` exits 2 as `usage`. `cli.Run` prints every error once, adding the JSON error
  envelope under `--format json|jsonl`.
- End-to-end scripts: `internal/cli/testdata/script/*.txtar`, run by `TestScripts` in
  [`internal/cli/script_test.go`](internal/cli/script_test.go) against a scratch `$WORK`
  and the fixed clock `CORONER_TEST_NOW`. Contract scripts live in
  [`contract/`](internal/cli/testdata/script/contract/README.md).
- Vocabulary: use the glossary in [`CONTEXT.md`](CONTEXT.md) for names in code, flags,
  JSON and docs. The noun is `annotation`, not `note`.
- Layering and function-shape rules live in lint ([`.golangci.yml`](.golangci.yml)), not
  here.

## Rules

1. **clig.dev is mandatory:** check every new or changed command, flag, output or error
   message against [clig.dev](https://clig.dev/) using
   [the command checklist](docs/contributing/command-checklist.md). Cite its rule IDs, as
   in "violates OUT-3".
2. **Never hand-edit generated files:** `internal/store/sqlcgen/**` and
   `docs/reference/cli/**` are regenerated by `mise run format`, whose hk steps run
   `sqlc generate` and `go run ./internal/tools/docgen`; `mise run lint` fails when the
   CLI reference is stale. Expected output in `internal/cli/testdata/script/*.txtar` is
   rewritten by `go test ./internal/cli -run 'TestScripts/script$' -update`; contract
   scripts change only as a deliberate contract change (Rule 3), through the `contract`
   subtest. `internal/store/testdata/compat/v*.db` is written once, from its release's
   tag, by `go run ./internal/tools/compatfixture -o <path>`, and never regenerated
   ([the migration policy](docs/contributing/migrations.md)). Each issue that adds a
   generated artifact, such as `schema/*.json`, adds its path and how to regenerate it
   here.
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
