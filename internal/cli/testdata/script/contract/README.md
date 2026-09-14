# Contract scripts

*Audience: anyone, human or agent, adding or changing a testscript under this directory or
`internal/cli/roundtrip_test.go`.*

Scripts here pin coroner's machine-facing contract: `--format json` and `jsonl` shapes,
exit codes, and every command's `--help` output, in `help.txtar`. Changing one is a
contract change: follow [AGENTS.md Rule 3](../../../../../AGENTS.md#rules), and
`STABILITY.md` (not in the repo yet — EXC-1303) once it lands. Two diffs here are not:
rewording `--help`, though removing or renaming a command or flag is one, and renumbering
IDs. `CODEOWNERS` requests the maintainer's review of a change others propose here. Error
`kind` strings in every script here are experimental until 1.0. They, and fields a
script's header marks experimental, are pinned so a change to them is deliberate, but
before 1.0 changing them needs neither the maintainer's approval nor a major bump.

Assert an exact exit code with `exits <code> coroner [args...]`, which runs like `exec`
and keeps stdout and stderr for the assertions that follow; `! exec` only proves the
command failed.

The clock is pinned by `CORONER_TEST_NOW`, and record IDs are numbered in order from a
counter file named by `CORONER_TEST_IDS`, so a script's output is the same on every run.
The counter is unlocked: never run coroner in the background. An ID's number is not part
of the contract, only an `id` or `*_id` key's presence, type and format: every ID a
command allocates, even in a failed or `--dry-run` call, advances the counter, so
`-update` rewrites renumbered IDs and a diff that only renumbers them is not a contract
change.

Pin each JSON document shape with `cmp stdout <name>.json` against a file in the script's
own archive. When a later command only shows a behaviour through one field, such as
`current`, `truncated` or which timeline was selected, check that field with a `stdout`
pattern preceded by `jsonvalid stdout`, which fails unless stdout is empty or holds only
JSON values. Output that differs by machine or build, such as the version or the schema
version, stays a pattern. A document that holds a path under `$WORK` may be compared whole
with `cmpenv` against a hand-written file, which `-update` never rewrites, so an additive
key or a renumbered ID means editing that file by hand. A `cmp` file may still hold
`coroner_version` `(devel)`: `runCoroner` reads the test binary's build info, which
`go test` never stamps with a version, so whole export documents can be compared. Match an
error envelope as the last line of stderr by pattern: its `kind` is the contract, its
`message` and `hint` are not.

`go test ./internal/cli -run 'TestScripts/script$' -update` refreshes only the scripts
outside `contract/`. `go test ./internal/cli -run 'TestScripts/contract$' -update`
rewrites the files `cmp` compares against here, never a `cmpenv` file or a pattern; review
`git diff -- internal/cli/testdata/script/contract` before committing.

`export-fixtures.txtar` writes the export fixtures. `exportfixture <name> <file>...` joins
the files, such as `stdout`, and compares them with
`testdata/export/v<format_version>/<name>` at the module root. Under `-update` it writes
only a fixture that does not exist yet and never rewrites one, so a changed export fails
against a frozen fixture instead of replacing it. Each section of the script starts its
own database, ID counter and clock, so a fixture's bytes never depend on another section.
A fixture failure that is not an export format change, such as renumbered IDs, is fixed in
the script, for example by giving the new command its own section or pre-seeding that
section's counter file, never in the fixture.

[`roundtrip_test.go`](../../../roundtrip_test.go) belongs to this suite, so changing what
it requires is a contract change too. Its `TestExportImportRoundTrip` exports
rapid-generated databases as JSONL, imports each export into an empty database and
requires exporting that database to give the same bytes. A spec beside it imports every
fixture and requires the export to equal the current `format_version`'s fixture of the
same name, taking attachment files from those `export-fixtures.txtar` archives. rapid
saves a failing case under `internal/cli/testdata/rapid/` and replays it at the start of
every later run, or alone with the `-rapid.failfile` it prints. Git ignores those files,
and rapid skips a file that another rapid version wrote, so a fix pins its bug with a
named spec instead.
