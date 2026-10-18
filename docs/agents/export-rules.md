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
   reference updated in the same change (`AGENTS.md` Rules 3 and 4). `export.Decode`
   requires every key of the current type, so a key added in a minor version is exempted
   for older minors in the same change, for example by a `since:"1.1"` struct tag that
   `checkKeys` honours against the header's minor.
5. **The package imports no command or storage code:** the `export-contract` depguard rule
   in [`.golangci.yml`](../../.golangci.yml) fails an import of `internal/store`,
   `internal/cli` or cobra.
6. **Export JSONL has one reader and one writer:** read it only through `export.Decode`,
   and write it only through `export.Records` and `export.WriteRecords`. A command that
   takes JSONL input, such as bulk add or import, never parses JSONL or checks a record's
   keys, formats or order itself, so the commands cannot drift apart. Domain checks a
   command needs beyond the export format, such as a known `precision`, stay the
   command's.
