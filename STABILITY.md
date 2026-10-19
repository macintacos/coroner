# Stability

*Audience: anyone who runs coroner from a script, an agent or an importer, and anyone who
changes a command, flag, environment variable or JSON output.*

coroner is meant to be lived with for years. This document says what it promises to keep
working, what it may change, and how it withdraws a promise. It is the rule the contract
tests, the pull request template and the help topics follow.

## Versioning

- **0.x through the MVP.** Until 1.0, a release may break a candidate-stable element, but
  never silently and never by removing it outright ([§ Before 1.0](#before-10)).
- **1.0 once the command tree has survived real incidents,** as the maintainer judges. 1.0
  makes every candidate-stable element stable ([§ Tiers](#tiers)).
- **v1 forever.** After 1.0, a stable element is never removed or changed incompatibly
  within v1; a deprecated one stays as a hidden alias ([§ Deprecation](#deprecation)), so
  aliases may accumulate for as long as v1 lasts. A v2 needs the module path
  `github.com/macintacos/coroner/v2`, and anyone who installed with
  `go install github.com/macintacos/coroner@latest` would silently stay on v1
  ([Go modules: major versions](https://go.dev/doc/modules/major-version)). So v2 is a
  last resort, not a scheduled event.
- **An unavoidable change to a stable or candidate-stable element's behaviour, such as a
  flag's default, ships opt-in first,** behind a flag or setting, the way Go ships them
  behind [`GODEBUG`](https://go.dev/doc/godebug). The default flips only in a release
  whose notes say so.

## Tiers

- **Stable** elements follow every rule in this document.
- **Experimental** elements may change or disappear in any release, with no deprecation
  window, and a change to one is never a break. Help text marks one as experimental, such
  as `(experimental)` in a flag's usage or "experimental" in a command's description.

An element is experimental only when help text or
[§ Experimental in the MVP](#experimental-in-the-mvp) says so; every other command, flag,
environment variable, config key and JSON shape is on the stable track, including one
added after this list was written. Before 1.0 no element is stable yet: those on the
stable track are candidate-stable, become stable at 1.0, and are already withdrawn only
through [§ Deprecation](#deprecation).

### Candidate-stable at 1.0

- The nouns and verbs of the MVP command tree.
- `-t`, `--db`, `--format` and `--json`, `-q`, `--no-color`, `--dry-run`, `--yes`, and
  `--if-not-exists`.
- The filter flags and their semantics.
- `CORONER_TIMELINE` and `CORONER_DB`.
- The JSON and JSONL shapes, including the export format.
- The exit codes.
- The current-timeline precedence: `-t`, then `CORONER_TIMELINE`, then
  `coroner timeline use`.

### Experimental in the MVP

- Import's flags.
- `--group-by`.
- The Markdown export's layout. Like every rendering it is also never a contract
  ([§ What is not breaking](#what-is-not-breaking)), so it never becomes stable.
- The natural-language time grammar. RFC 3339 is stable; scripts send RFC 3339.
- The JSON fields of `status` and `version`.
- Error `kind` strings. They become stable at 1.0.

## What is breaking

For a stable or candidate-stable element, each of these is a breaking change:

- Removing or renaming a command, flag, environment variable or config key.
- Changing a flag's default, type or repeat semantics.
- Changing filter semantics, such as inclusive or exclusive bounds, or AND or OR.
- Removing, renaming or retyping a JSON key, or changing its nullability.
- Changing the timestamp format or the shape of an envelope.
- Changing the meaning of an error kind. An exit code's meaning never changes at all
  ([§ Exit codes](#exit-codes)).
- Changing the default sort order.
- Rejecting input that used to be accepted.

## What is not breaking

- Human text output and its layout.
- The Markdown, Mermaid, SVG and HTML renderings. An HTML export's embedded JSON is the
  JSON export, which is a contract.
- The wording of help and error messages.
- The results of natural-language time parsing.
- New commands, flags, JSON keys, JSONL record types, enum values, exit codes or error
  kinds.
- Relaxed validation.
- The SQLite schema, which is internal. A database an older coroner wrote still opens in
  every later release ([the migration policy](docs/contributing/migrations.md)), but only
  exports are shared.

## JSON consumers

Scripts, agents and importers that read any of coroner's JSON must follow these rules.
They keep a script working across additions; they do not protect it from a change to an
experimental field, such as `status`'s, which may change outright.

- **Ignore keys and JSONL record types they do not know.** New ones are added over time.
- **Accept enum values they do not know,** such as a new `type`, `precision` or error
  `kind`.
- **Branch on the exit code, or on error `kind` knowing it may change before 1.0, never on
  message text.**

An export's `format_version` is `MAJOR.MINOR`, versioned apart from coroner itself, with
Terraform's semantics: a minor version only adds, and anything else is a new major
version. Readers check the major version.
[The export format's evolution rules](docs/reference/export-format.md#evolution-rules)
list what each may do.

## Exit codes

coroner exits 0 on success and non-zero on failure. A script **must** treat an exit code
it does not know as a failure: new codes may be added. A code's meaning never changes, not
even through [§ Deprecation](#deprecation): a new meaning gets a new code.
`coroner help exit-codes` lists every code and the error kinds under it.

## Deprecation

A stable or candidate-stable element is withdrawn by deprecating it first, never by
removing it outright.

1. **Deprecate it in favour of something at least as stable.** A stable element is never
   deprecated in favour of an experimental one.
2. **Warn whenever the command line parses.** Mark the command with `deprecateCommand` or
   the flag with `deprecateFlag`
   ([`internal/cli/deprecation.go`](internal/cli/deprecation.go)). Each panics on a notice
   that does not name the replacement, the earliest removal version and the earliest
   removal date, and cobra prints the notice on stderr:

   ```text
   Flag --old-name has been deprecated, use --new-name instead; it may be removed in v0.9.0 or later, and no earlier than 2027-03-14
   ```

   The notice never reaches stdout, so `--json` output stays valid. That holds only while
   no command is left with cobra's Out set, so never call `root.SetOut` outside the
   completion setup that resets it. A spec in
   [`internal/cli/root_test.go`](internal/cli/root_test.go) and
   [`deprecation.txtar`](internal/cli/testdata/script/deprecation.txtar) fail when one
   does. A shorthand alone, an environment variable or a config key has no helper yet; its
   notice goes to stderr and names the same three things.
3. **Keep it working for at least 6 months and at least one minor release,** both: its
   removal date is at least 6 months after the release that first ships the notice, and
   its removal version at least the next minor version after that release. An element
   first deprecated in v0.5.2 on 2027-01-10 may go no earlier than v0.6.0 and 2027-07-10.
   An experimental element needs no window.
4. **Hide it from help, and list it here.** Deprecating hides it from `--help` and the
   generated CLI reference, so add it to [§ Currently deprecated](#currently-deprecated),
   with its replacement and removal version and date. Remove a deprecated command's
   `Example`, since the examples spec
   ([`internal/cli/examples_test.go`](internal/cli/examples_test.go)) rejects an example
   that invokes a deprecated command or flag, and move every example, help topic and docs
   page that uses the element to its replacement.
5. **Announce it in the release notes.** Name the deprecated element and its replacement
   in the pull request's body; the release notes list it under **Deprecated**. Deprecating
   is not a break, since the element keeps working, so it is never listed under
   **Breaking**.
6. **Remove it only once its window has passed.** Before 1.0, removing it is a break
   ([§ Before 1.0](#before-10)). After 1.0 a stable element is never removed within v1: it
   stays as a hidden alias that keeps working and keeps warning, until a major version
   (see [§ Versioning](#versioning)). That includes an element still deprecated when 1.0
   ships, which becomes stable with the rest. So after 1.0 a notice's removal version is
   the next major, v2.0.0, and a notice naming an earlier one when 1.0 ships moves to
   v2.0.0 in that release.

This policy adapts the
[Kubernetes deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/)
rules 5a, 5c and 6, with a shorter window.

## Before 1.0

- A release may break a candidate-stable element, within two limits: it is withdrawn only
  through [§ Deprecation](#deprecation), to practise the process before 1.0 makes it
  binding, and a change to its behaviour ships opt-in first ([§ Versioning](#versioning)).
  An experimental element may change freely ([§ Tiers](#tiers)).
- Every break, such as removing a candidate-stable element once its window has passed, is
  listed under the **Breaking** heading of the release notes, and its pull request's
  `stability:` line says `breaking`. A change to an experimental element is not a break,
  so it is not listed there.

## Classifying a contract change

A pull request that makes a contract change, as
[the contract README](internal/cli/testdata/script/contract/README.md) defines it, names
its stability in the pull request template, taking the first label that fits the whole
diff:

- **breaking**: it makes any change on [§ What is breaking](#what-is-breaking) to a stable
  or candidate-stable element. The release notes list it under **Breaking**
  ([§ Before 1.0](#before-10)).
- **additive**: otherwise, it adds something from
  [§ What is not breaking](#what-is-not-breaking), such as a new key, record type, enum
  value, command, flag, exit code or error kind, whatever else it changes in experimental
  elements.
- **experimental**: otherwise, it changes only experimental elements, such as an error
  `kind` string before 1.0 or a field a contract script's header marks experimental.

Deprecating a stable-track element, which hides it from `help.txtar`, is **additive**. A
pull request that makes no contract change, such as rewording `--help`, only renumbering
IDs, or only changing the Markdown export's headings, takes no label.

## Currently deprecated

Nothing is deprecated.
