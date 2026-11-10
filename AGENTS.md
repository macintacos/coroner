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
  `sqlc generate`, the CLI reference generator and the export schema generator.
- `mise run lint` runs the linters without writing, then fails if the CLI reference or the
  export schema is stale. It does not check YAML, TOML, shell, Pkl or Python formatting,
  `go mod tidy` or `sqlc generate`; only `mise run format` and `pre-commit` apply those.
- `mise run test` runs the Ginkgo suites with `go tool ginkgo -p`, and with them the
  testscript end-to-end scripts and the rapid properties; a package pattern narrows it,
  and `--focus <regexp>` narrows Ginkgo specs only, so plain `Test*` functions such as
  `TestScripts` still run. The ginkgo CLI caches no results; `go test ./...` runs the same
  tests and reuses a package's cached result while its code and the files its tests read
  are unchanged (`go test -count=1 ./...` reruns everything), so the `pre-push` hook and
  CI run that. Under `-p` ginkgo starts one copy of a test binary per process, and
  `TestMain` in [`script_test.go`](internal/cli/script_test.go) keeps `internal/cli`'s
  plain `Test*` functions to the first, since each copy would otherwise run them all.
- `mise run docs:serve` serves the docs site at `http://localhost:2676` and rebuilds it on
  change. `mise run docs:build` builds it into `site/` with Zensical through `uv`, failing
  on any warning, such as a broken link between pages, copies the export schema in, and
  fails when a page links a file under `/coroner/schema/` that is not in `site/`.
- `mise run preflight` runs lint, test, build and docs:build, starting docs:build only
  once lint and test finish, since both walk the tree and fail when it rebuilds `site/`
  under them. It is the gate before declaring work done (Rule 6).
- `mise run release:check` validates [`.goreleaser.yaml`](.goreleaser.yaml), and
  `mise run release:snapshot` builds every release artifact into `dist/` without
  publishing. `/release-coroner` runs `release:check` and `release:snapshot` as release
  gates, and [the releasing guide](docs/contributing/releasing.md) describes the flow.

The [`hk.pkl`](hk.pkl) hooks run on their own: `pre-commit` runs every formatter and
linter and applies fixes, regenerating the CLI reference when a help topic or the Go code
it documents changes, and the export schema when the export types change; and `pre-push`
runs `go test ./...` when Go files, `go.mod`, `go.sum` or anything under `internal/`,
`docs/`, `skills/`, `testdata/` or `schema/` change.

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs on every pull request and
push to `main`: [`.github/scripts/docs-gate-test`](.github/scripts/docs-gate-test), which
checks the docs gate's cases, `mise run lint`, `go test ./...`, `mise run docs:build`,
then `mise run format` failing on any diff, so a commit made with `--no-verify` still gets
a red check. It restores the Go module, build and golangci-lint caches from the newest
green push to `main`, and only such a push saves them; the build cache holds test results,
which a package reuses because CI first sets each checked-out file's mtime from its
content. The docs gate
([`.github/workflows/docs-gate.yml`](.github/workflows/docs-gate.yml)) fails a pull
request that changes a file under `internal/cli/` other than a test, testdata or help
topic without changing a page under `docs/` outside `docs/reference/cli/` and
`docs/rules/`, unless it has the `docs-not-needed` label;
[`.github/scripts/docs-gate`](.github/scripts/docs-gate) holds the rule and reads changed
paths on stdin. Pushing a `v*` tag runs two workflows: one deploys the docs site to GitHub
Pages ([`.github/workflows/docs.yml`](.github/workflows/docs.yml)), for a final tag only,
not a release candidate such as `v0.1.0-rc.0`, and the release workflow
([`.github/workflows/release.yml`](.github/workflows/release.yml)) reads the release notes
from the annotated tag's message, then has goreleaser publish the release and update the
Homebrew cask.

## Routing

Walk the `router` fence from `@working-on` as a checklist, not a single path: take
**every exit whose label matches the change**, and load each file those stops name before
editing that area. A stop marked "not in the repo yet" names the issue that will land that
file. Until it lands there is nothing to load: skip the stop, and never create its file
early.

```router
@working-on: Working on coroner
  > what-does
what-does: What does the change touch?
  any change: the repository rules always apply > agents
  a command, flag, output or error message (Rule 1) > command-checklist
  Go code: function shape and layering are lint rules > load-golangci
  Go code: what to log, at which level, under which names, and what never to log > logging
  naming something in code, flags, JSON or docs > context
  JSON output, exports or another contract (Rule 3) > stability
  a command, flag, JSON key, exit code or experimental surface the agent skill or its references name (Rule 5) > skill
  adding, removing, renaming or changing a doc, a mise task, hk hook or CI workflow step, a docs/rules/ file or a generated artifact; an area's first rules; landing anything marked not in the repo yet; or user-visible behaviour (Rule 4) > documentation
  internal/timeline, or code that stores, orders or validates its types > timeline
  internal/store: a migration, a query, sqlc.yaml, the SQL it runs, or the store's transactions and row mapping > store
  internal/timeparse, or code that turns typed time input into an instant > timeparse
  internal/timetext, or code that shows an event's time to people > timetext
  internal/export, schema/, testdata/export/, or code that encodes the export types, reads export JSONL or renders the Markdown, Mermaid, HTML or SVG export > export
  internal/viewer, or the code in internal/cli that loads what it shows > viewer
  skills/: the agent skill, its references, skills/skills.go or .claude-plugin/marketplace.json > skills
  internal/tools/release: a subcommand, flag, JSON key or error code the /release-coroner skill uses > skill-2
  how coroner looks or reads to people: the HTML or SVG export's styling, the viewer, picker or forms, how command output and errors are styled, or the docs site's theme > readme
agents!: Load AGENTS.md § Rules
command-checklist!: Load docs/contributing/command-checklist.md
load-golangci!: Load .golangci.yml
logging!: Load docs/rules/logging.md
context!: Load CONTEXT.md
stability!: Load STABILITY.md
skill!: Load skills/coroner/SKILL.md
documentation!: Load docs/rules/documentation.md
timeline!: Load docs/rules/timeline.md
store!: Load docs/rules/store.md
timeparse!: Load docs/rules/timeparse.md
timetext!: Load docs/rules/timetext.md
export!: Load docs/rules/export.md
viewer!: Load docs/rules/viewer.md
skills!: Load docs/rules/skills.md
skill-2!: Load .claude/skills/release-coroner/SKILL.md
readme!: Load design/README.md
```

## Where things live and what to call them

- Commands: `internal/cli`, one package per top-level command, each a `NewCommand`: the
  nouns (`event`, `timeline`, `annotation`, `attachment`, `source`, `tag`, `overlay`), a
  file per verb but `overlay`, one file, and the verbs `status`, `version`, `agent`,
  `help`, `importcmd` (`import`, a Go keyword) and `internalcmd` (`__internal`, since a
  directory named `internal` limits who may import it). The root package holds only
  `NewRoot` and `Run`, and the specs that walk the whole command tree. A command's own
  specs sit beside it as an external test package (`package event_test`), which may import
  the root to run the whole CLI; `internal/cli/clitest` holds what they share: the
  `Harness` that builds a spec's `App`, a database at the latest schema, and readers of
  what a command printed. It is test code, so it never imports the root and no command
  imports it. What commands share lives in `internal/cli/clikit`: `App`, `Error`, output,
  selection, definitions, forms and logging. `internal/cli/merge` plans and writes what
  `import` and `event add --from-file` merge. A command package imports `clikit` and
  `merge`, never another command's package or the root; the root hands `event` and
  `annotation` the form step that creates a timeline (`timelinecmd.AskCreate`). Store:
  `internal/store`. Domain types: `internal/timeline`. Typed time input:
  `internal/timeparse`. How an event's time reads to people: `internal/timetext`. Config
  file, XDG paths and precedence, and the attachment blob directory and the diagnostic
  log's path (`config.DiagnosticLogPath`) beside the database: `internal/config`, whose
  spec fails when [`docs/reference/configuration.md`](docs/reference/configuration.md)
  misses a key the config file accepts. JSON export types, envelope, encoder and JSONL
  decoder, and the Markdown, Mermaid, HTML and SVG exports' renderers: `internal/export`,
  the JSON described by [the export format reference](docs/reference/export-format.md);
  the HTML page's viewer and vendored vis-timeline live in `internal/export/html/`, its
  colour tokens in `internal/export/theme.go`, and the SVG picture's template in
  `internal/export/svg/`. The terminal viewer `timeline view` opens and the picker
  `timeline list` opens: `internal/viewer`. Which terminals show images inline, the Kitty
  graphics sequences and the placeholder text shown where none can: `internal/graphics`,
  pure. `main.go` builds `clikit.App` from the process; code below it takes everything
  from `clikit.App`, never from `os`. Dev tools run with `go run`: `internal/tools/`
  (`docgen` writes the CLI reference, and with `--man <dir>` the man pages a release
  packs; `schemagen` the export schema; `compatfixture` a release's fixture database;
  `release` the deterministic steps of `/release-coroner`, plus the release workflow's
  `tag-notes` step).
  [`.claude/skills/release-coroner/`](.claude/skills/release-coroner/SKILL.md) is the
  maintainer-only project skill that cuts a release; it ships neither with `skills/` nor
  in the marketplace.
- Help topics: `internal/cli/help/topics/<topic>.md`, embedded and printed as-is by
  `coroner help <topic>`, and copied into the CLI reference by `docgen`; those pages are
  also the site's reference pages for the topics. Specs fail when `exit-codes` disagrees
  with the exit-code map or `environment` misses an environment variable the code reads.
  The `reference` topic is not a file: `writeReference`
  ([`reference.go`](internal/cli/help/reference.go)) renders it from the command tree, so
  `docgen` writes no page for it.
  [`help-reference.txtar`](internal/cli/testdata/script/help-reference.txtar) pins the
  page, and any command or flag change rewrites it.
- Global flags and environment: `ReadInvocation`
  ([`invocation.go`](internal/cli/clikit/invocation.go)) reads them once at the top of a
  RunE, and resolves `--tz` only when a command asks for the zone. `agent skill`,
  `version`, shell completion and `ErrorStyleFor`, which runs when cobra failed to parse,
  never call it, so a malformed config file cannot break them.
- Errors and exit codes: a command's hooks or body return a `*clikit.Error` whose `Kind`
  picks the exit code; `ExitCodes` in
  [`internal/cli/clikit/errors.go`](internal/cli/clikit/errors.go) is the table. A domain
  validation problem becomes an exit kind in one table, `KindOf` in
  [`internal/cli/clikit/validation.go`](internal/cli/clikit/validation.go); commands turn
  a record's `Validate` error into an `*Error` with `RecordError`, and `storeError` routes
  a store-side `*timeline.ValidationError` through it. Any other error, or a panic, exits
  1 as `internal`. A command declares the output formats it supports in its `formats`
  annotation, and any other `--format` or `--json` exits 2 as `usage`. `-t` names one
  timeline, and naming more exits 2 as `usage`, unless the command reads several, as
  `event list`, `timeline export`, `timeline view` and `overlay create` do. A command that
  reads `-t`, `--overlay`, `CORONER_TIMELINE` or the current timeline selects only through
  [`selection.go`](internal/cli/clikit/selection.go): `ReadTimelineInput` reads the
  command line before the store opens (from `wrapRunErrors` and `ReadInvocation`),
  `TimelineInputOf` builds the same input from a write command's or form's fields, and
  `SelectTimelines` selects against the store. A command declares how many timelines it
  reads in its `timelines` annotation (`several`, `positional`, `guard`), and the formats
  that print only one in its `oneTimelineFormats` annotation. A command whose
  `DiagnosticLogAnnotation` is `off` keeps it and every command under it out of the
  diagnostic log; a spec in
  [`diagnostic_log_internal_test.go`](internal/cli/diagnostic_log_internal_test.go) pins
  the opted-out set (`agent skill`, `completion`, `version`, `__internal`), and a new
  opted-out command joins it. `event list` and `timeline export` also take `--overlay`, a
  saved overlay's name, in place of `-t`, and read their events through
  `store.EventFilter.TimelineIDs`. `cli.Run` prints every error once, adding the JSON
  error envelope under `--format json|jsonl`.
- Deprecating a command or flag: `DeprecateCommand` and `DeprecateFlag` in
  [`internal/cli/clikit/deprecation.go`](internal/cli/clikit/deprecation.go), under the
  process in [`STABILITY.md`](STABILITY.md#deprecation).
  [`deprecation.txtar`](internal/cli/testdata/script/deprecation.txtar) runs a throwaway
  deprecated flag to prove its notice stays off stdout.
- Write commands (`timeline create|edit|delete|use`, `event add|edit|delete|link|unlink`,
  `annotation add|edit|delete`, `attachment delete`, `overlay create`, `source edit`):
  each declares its flags, positional argument and their checks once, in a `Definition`
  ([`internal/cli/clikit/definition.go`](internal/cli/clikit/definition.go)), which
  registers the flags and builds the `huh` form fields
  ([`form.go`](internal/cli/clikit/form.go)). A field also marks, with `Changes`, whether
  it changes the record, which an edit needs at least one of, with `PerEntry`, whether
  another form entry opens without it, with `Title`, what the form calls it, and with
  `More`, that the form asks it behind More options. `FieldIcon` gives each field name the
  icon its title shows; a spec in
  [`form_internal_test.go`](internal/cli/form_internal_test.go), which lists each form
  command's form definition, fails when one of its fields has none, and a new form command
  joins that list.
  [`add_form_keys_internal_test.go`](internal/cli/add_form_keys_internal_test.go) is the
  `testing/synctest` spec that presses a form's keys and pins `event add`'s key counts.
  `RunE` reads the definition's values, calls a plain run function that returns a result,
  and renders it. The run function does its find, apply and write in one `WithChange`
  ([`store.go`](internal/cli/clikit/store.go)), and `--dry-run` is that change rolled
  back; `import` and `--from-file` instead merge through `merge`, which plans and writes
  in one `Change` and plans a dry run in one `Read`. Specs in
  [`definition_internal_test.go`](internal/cli/definition_internal_test.go) fail when a
  flag has no form field, a form field has no flag, or the two reject a bad value
  differently; when a command a write verb names has neither a definition nor a
  `definitionExempt` entry; and when an edit command's field is neither its argument,
  inherited, `--dry-run` nor marked `changes`. One in
  [`write_commands_internal_test.go`](internal/cli/write_commands_internal_test.go) fails
  when a command named by a write verb from the verb glossary lacks `--dry-run` or
  `--json`. A create prints only its new id under `-q` through `Output.PrintID`
  ([`output.go`](internal/cli/clikit/output.go)). On a terminal, `event add`,
  `annotation add`, `timeline create` and `timeline edit` open that form, attached through
  `FormCommand` ([`add_form.go`](internal/cli/clikit/add_form.go)), run each submit
  through the same run function, and print the flags-only command it equals
  ([`command_echo.go`](internal/cli/clikit/command_echo.go)). `interactiveTerminal` in
  [`invocation.go`](internal/cli/clikit/invocation.go) is the terminal decision every
  form, the viewer and the picker share: stdin and stdout on a terminal, `TERM` not
  `dumb`, and no `--no-input` or `CORONER_NO_INPUT`. Each command also checks its format:
  under `--json` a form opens only with `--interactive`.
  [`no-input.txtar`](internal/cli/testdata/script/no-input.txtar) runs each such command
  with no terminal, and a spec in [`no_input_test.go`](internal/cli/no_input_test.go)
  fails when a command with `--interactive`, or `timeline view` or `timeline list`, which
  it lists by name, has no run there; a new command that opens a viewer or picker joins
  that list.
- `import` ([`internal/cli/importcmd/importcmd.go`](internal/cli/importcmd/importcmd.go)),
  a top-level verb since it ingests every record type (S-2's glossary lists `import`), and
  `--from-file` read JSONL only through `export.Decode`, and merge through `merge.Merge`
  ([`merge.go`](internal/cli/merge/merge.go)). `merge` decides what to insert, skip or
  replace in `PlanImport` ([`plan.go`](internal/cli/merge/plan.go)), which reads the
  database only through the lookup it is handed and does no other I/O; `merge` then writes
  the plan with `Tx.Import` in the same `Change`. A dry run plans in one `Read`, or, when
  the database does not exist yet, against the empty `StoredRecords`. `import` takes a
  file rather than typed values, so it declares plain flags and no `definition`, and opens
  no form. `event add --from-file` ([`add_file.go`](internal/cli/event/add_file.go)) is
  its insert-only subset: event records through the same `export.Decode`, `merge` and
  `PlanImport`, never a second reader or validator. It is `event add`'s one flag outside
  its definition, exempted by name in the form parity spec. `event add --id` shares the
  "same event" rule through `EventDiff`.
- End-to-end scripts: `internal/cli/testdata/script/*.txtar`, run by `TestScripts` in
  [`internal/cli/script_test.go`](internal/cli/script_test.go) against a scratch `$WORK`,
  the fixed clock `CORONER_TEST_NOW` and the ID counter `CORONER_TEST_IDS`. Contract
  scripts live in [`contract/`](internal/cli/testdata/script/contract/README.md).
- Export fixtures: `testdata/export/v<MAJOR.MINOR>/` holds real exports of each released
  format version, written by
  [`contract/export-fixtures.txtar`](internal/cli/testdata/script/contract/export-fixtures.txtar)
  and validated, decoded and re-encoded by
  [`internal/export/fixtures_test.go`](internal/export/fixtures_test.go), then imported
  and exported again by
  [`internal/cli/roundtrip_test.go`](internal/cli/roundtrip_test.go), whose rapid property
  requires a generated database's export to survive import byte for byte;
  `testdata/export/compat/` holds hand-written exports of other versions.
- Examples: [`internal/cli/examples_test.go`](internal/cli/examples_test.go) parses every
  `coroner` line in every command's `Example`, and in the `sh`, `bash`, `shell` and `zsh`
  code blocks and the `$` lines of `console` blocks on every page under `docs/` and
  `skills/`, against the command tree, without running it. A line where `coroner` is not a
  command word, as in `brew uninstall --cask coroner`, is reported as skipped.
- Agent skill: [`skills/coroner/`](skills/coroner/SKILL.md) holds the Agent Skill and its
  `references/`, with spec front matter keys only. Go cannot embed a parent directory, so
  [`skills/skills.go`](skills/skills.go) embeds `SKILL.md`; `main.go` passes it in as
  `App.Skill`, and `coroner agent skill` ([`agent.go`](internal/cli/agent/agent.go))
  prints it (S-2's glossary lists it). A spec fails when `agent skill` prints anything but
  the file or opens the database, and two others when `SKILL.md`'s front matter breaks or
  a link to a reference does. The repository is its own Claude Code plugin marketplace:
  [`.claude-plugin/marketplace.json`](.claude-plugin/marketplace.json) offers `skills/` as
  a plugin holding only this skill.
- Debugging coroner itself, not an incident: commands live under the hidden `__internal`
  group ([`internalcmd.go`](internal/cli/internalcmd/internalcmd.go)), which stays out of
  the diagnostic log and the stability contract. Specs fail when it shows in root help,
  the `reference` topic, shell completion or the CLI reference. `__internal log`
  ([`log.go`](internal/cli/internalcmd/log.go)) prints the diagnostic log as text. A
  hidden command gets no CLI reference page, so its flags are documented on the reference
  page of what it reads, as `__internal log`'s are in `docs/reference/diagnostic-log.md`.
- Docs site: `docs/` is the site's source, laid out as `tutorial/`, `how-to/`,
  `reference/`, `explanation/` and `contributing/`, with its nav in
  [`zensical.toml`](zensical.toml). Zensical is a Python dependency in
  [`pyproject.toml`](pyproject.toml), locked by `uv.lock`. Every file under `docs/` is
  published except the agent-facing rules in [`docs/rules/`](docs/rules/), which
  `zensical.toml`'s `exclude_docs` leaves out, so a published page links a file there, or
  outside `docs/`, by its GitHub URL, except the export schema, which `docs:build` copies
  into the site at `/coroner/schema/`, under `site_url`'s path, and fails when a page
  links a file there it did not copy.
- Vocabulary: use the glossary in [`CONTEXT.md`](CONTEXT.md) for names in code, flags,
  JSON and docs. The noun is `annotation`, not `note`.
  [`docs/explanation/data-model.md`](docs/explanation/data-model.md) carries the glossary
  word for word, and a spec fails when the two differ.
- Layering and function-shape rules live in lint ([`.golangci.yml`](.golangci.yml)), not
  here.

## Rules

1. **clig.dev is mandatory:** check every new or changed command, flag, output or error
   message against [clig.dev](https://clig.dev/) using
   [the command checklist](docs/contributing/command-checklist.md). Cite its rule IDs, as
   in "violates OUT-3".
2. **Never hand-edit generated files:** `internal/store/sqlcgen/**`,
   `docs/reference/cli/**` and `schema/*.json` are regenerated by `mise run format`, whose
   hk steps run `sqlc generate`, `go run ./internal/tools/docgen` and
   `go run ./internal/tools/schemagen`; `mise run lint` fails when the CLI reference or
   the export schema is stale. Expected output in `internal/cli/testdata/script/*.txtar`
   is rewritten by `go test ./internal/cli -run 'TestScripts/script$' -update`; contract
   scripts change only as a deliberate contract change (Rule 3), through the `contract`
   subtest. The viewer's screens in `internal/viewer/testdata/*.golden` are rewritten by
   `go test ./internal/viewer -update`. The Markdown, Mermaid, HTML and SVG exports'
   goldens in `internal/export/testdata/markdown/*.md`,
   `internal/export/testdata/mermaid/*.mmd`, `internal/export/testdata/html/*.html` and
   `internal/export/testdata/svg/*.svg` are rewritten by
   `go test ./internal/export -update`. The page-exported annotations file in
   `internal/cli/testdata/script/viewer-annotations.txtar` is rewritten only by importing
   that script's `inc-4821.jsonl`, exporting it with `--format html`, and exporting two
   comments from that page in a browser under the name `Priya Shah`, which the script
   reads as `inc-4821-annotations-Priya-Shah.jsonl`; its `annotations.json` is then
   rewritten by `go test ./internal/cli -run 'TestScripts/script$' -update`.
   `internal/store/testdata/compat/v*.db` is written once, from its release's tag, by
   `go run ./internal/tools/compatfixture -o <path>`, and never regenerated
   ([the migration policy](docs/contributing/migrations.md)).
   `testdata/export/v<MAJOR.MINOR>/` is written for the current `format_version` by
   `go test ./internal/cli -run 'TestScripts/contract$' -update`, which writes only a
   fixture that does not exist yet; before the first release that ships export, a fixture
   is regenerated by deleting it first. A release's shell completions and man pages are
   generated by `.goreleaser.yaml`'s `before` hooks into the ignored `completions/` and
   `manpages/` and never committed. `uv.lock` is rewritten by `uv lock`, or by `uv add`
   when a dependency changes; `mise run docs:build` fails when it is stale. Each issue
   that adds a generated artifact adds its path and how to regenerate it here.
3. **JSON output is a contract:** change it by editing the Go types, regenerating the
   schema and adding a fixture; an additive change bumps `format_version` minor, and any
   other change needs the maintainer's explicit approval and a major bump. Export fixtures
   (`testdata/export/v*/`) are never edited or removed once a release ships them, and a
   format change adds its version's directory beside them; human text and the Markdown,
   Mermaid, HTML and SVG layouts are not contracts.
4. **User-visible behaviour changes docs in the same PR:** update the relevant how-to or
   reference page, and the stability document when a contract is affected. The docs gate
   is only a backstop for CLI changes; label a pull request `docs-not-needed` only when
   nothing a user sees changed.
5. **Keep the agent skill in sync:** when a command, flag, JSON key or exit code used in
   [`skills/coroner/`](skills/coroner/SKILL.md) changes, update the skill in the same PR.
   The examples test catches only a command or flag that no longer parses.
6. **Run `mise run preflight` before declaring work done:** it runs lint, test, build and
   docs:build ([`.mise/tasks/preflight`](.mise/tasks/preflight)).
