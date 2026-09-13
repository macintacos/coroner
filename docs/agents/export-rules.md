# Export rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **Every key is always present:** no field of an `internal/export` type uses `omitempty`
   or `omitzero`. A key that may have no value is a pointer tagged
   `jsonschema:"nullable"`, and a collection is a slice or map, which encodes as `[]` or
   `{}` when empty. A schemagen spec fails when a pointer field lacks the tag.
2. **An instant is an `export.Instant`,** never a `string` or a `time.Time`: its
   `MarshalText` is what routes the value through `timeline.FormatInstant`.
3. **An `internal/export` value is encoded with `json.Deterministic(true)` and
   `jsontext.EscapeForHTML(true)`,** never `encoding/json` v1. `export.Write` applies both
   and writes nothing when encoding fails; a caller needing another presentation, such as
   indentation, adds options without dropping those two.
4. **A changed type is a contract change:** edit the Go type, run
   `go run ./internal/tools/schemagen`, and classify the change by the evolution rules in
   [the export format reference](../reference/export-format.md#evolution-rules), with the
   reference updated in the same change (`AGENTS.md` Rules 3 and 4). `export.Decode` and
   the schema require every key of the current type, so the first key added in a minor
   version carries a `since:"1.N"` struct tag, and that change teaches `checkKeys` to skip
   a tagged key for a header of an older minor and schemagen to leave it out of
   `required`. Neither exists yet; until they do, a new required key fails every older
   minor's fixtures (rule 7). With them, an older minor's fixture still validates and
   decodes, but no longer re-encodes to its own bytes: the added key is written into its
   lines.
5. **The package imports no command or storage code:** the `export-contract` depguard rule
   in [`.golangci.yml`](../../.golangci.yml) fails an import of `internal/store`,
   `internal/cli` or cobra.
6. **Export JSONL has one reader and one writer:** read it only through `export.Decode`,
   and write it only through `export.Records` and `export.WriteRecords`. A command that
   takes JSONL input, such as bulk add or import, never parses JSONL or checks a record's
   keys, formats or order itself, so the commands cannot drift apart. Domain checks a
   command needs beyond the export format, such as a known `precision`, stay the
   command's.
7. **Every released fixture keeps passing:** `internal/export/fixtures_test.go` lists
   every file in `testdata/export/v*/`, so a new fixture joins that list. Every fixture,
   an older minor's included, must still validate against the schema and decode under the
   current types. Only the current `format_version`'s fixtures are re-encoded, the `.json`
   one through `export.Write` and the JSONL through `export.Records`, and compared byte
   for byte, because a later minor's added keys legitimately re-encode into an older
   minor's lines. A failure that is an export format change is a breaking change, fixed in
   the Go types and never in the fixture (`AGENTS.md` Rules 2 and 3). A fixture failure
   that is not an export format change, such as renumbered IDs, is fixed in the script,
   for example by giving the new command its own section or pre-seeding that section's
   counter file, never in the fixture. A format change's new fixture directory or schema
   diff lands in a commit whose type and subject describe the format change, which is what
   puts it in the changelog. `testdata/export/compat/` holds hand-written exports of other
   versions that pin the evolution rules; they are not frozen.
8. **The Markdown export has one writer:** render it only through `export.WriteMarkdown`,
   from the `Envelope` that `json` and `jsonl` encode, so every format exports the same
   events. Its layout is not a contract (`AGENTS.md` Rule 3), so it changes without a
   version bump, but always within the limits that keep it pasting cleanly into chat and
   wikis: plain CommonMark with no tables, front matter or raw HTML, and lists at most two
   levels deep. Specs parse every golden in `internal/export/testdata/markdown/` with
   goldmark to check that, and that no heading repeats under one parent, and run
   `rumdl check` on them with MD013 off. `rumdl fmt` skips that directory, so a golden
   changes only through `go test ./internal/export -update`, with its diff read before
   committing.
