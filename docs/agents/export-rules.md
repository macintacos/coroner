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
   the schema require every key of the current type, so a key added to an existing object
   in a minor version carries a `since:"1.N"` struct tag, which schemagen reads to leave
   the key out of `required`, as `Envelope.Attachments` does. A new record type's keys
   need no tag: no older minor has that record. `checkKeys` does not read the tag yet, so
   the first tagged key on a JSONL record type or the header teaches it to skip that key
   for a header of an older minor; until then such a key fails every older minor's JSONL
   fixtures (rule 7). An older minor's fixture still validates and decodes, but no longer
   re-encodes to its own bytes: the added key is written into its lines.
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
   wikis: plain CommonMark with no tables, front matter or raw HTML, lists at most two
   levels deep, and URL tokens left unescaped so autolinks keep them whole. An
   attachment's image or link destination is built only from its hash, by
   `export.AttachmentPath` or the caller's `AttachmentLink`, never from its filename. The
   filename is only the text, and goes through the escaper, not `escape`, whose unescaped
   URLs could hold a `]`. An event's attachments are list items beside its annotations,
   and an annotation's attachments follow its text on its line, so neither nests a third
   level. Specs parse every golden in `internal/export/testdata/markdown/` with goldmark
   to check there is no raw HTML, lists are at most two levels deep and no heading repeats
   under one parent, and run `rumdl check` on them with MD013 off. Its MD057 needs every
   relative link to resolve, so `attachments/` there holds an empty file for each
   attachment hash the goldens link, as an `-o` directory export would. `rumdl fmt` skips
   that directory, so a golden changes only through `go test ./internal/export -update`,
   with its diff read before committing.
9. **The Mermaid export has one writer:** render it only through `export.WriteMermaid`, or
   `export.WriteMarkdown` with `Diagram`, from the same `Envelope`, so the diagram shows
   the events every other format exports. Its layout is not a contract (`AGENTS.md` Rule
   3), but it keeps to gantt syntax that hosts running an old Mermaid still draw:
   second-precision `dateFormat`, and no `tickInterval`, `displayMode: compact`, `crit` or
   colours. Every piece of text goes through the diagram's escaper, which writes `#`, `:`,
   `%` and `;` as entities; Mermaid before 10.8.0 reads title, section and task text only
   up to `;`, `#` or `:`, and GitHub does not publish its Mermaid version. A task label
   that starts, after any whitespace, with what Mermaid's gantt lexer reads before task
   text, such as `section`, `title`, `click` or a date, has its first character written as
   an entity too; the guard counts as whitespace what a JavaScript regexp does, such as
   NBSP, `\v`, U+FEFF and the Unicode spaces, because the lexer is one. Unescaped, such
   text is silently dropped, starts a section, replaces the title or fails to parse. A bar
   is written as its start and a duration in seconds, `:<start>, <N>s`, never an end date:
   on the night a zone falls back, a wall-clock end can come before its start, and Mermaid
   silently draws no bar. Mermaid dates carry no offset, and a location named `Local` says
   nothing to a reader, so the title names the zone by the abbreviations its times carry
   on the zone's wall clock, such as `(EDT/EST)`, as the Markdown export labels times. A
   diagram over `export.MermaidMaxTextSize` is still written whole, with a warning on
   stderr, because cutting it would silently drop events. The escaper and the guard were
   checked by rendering in Mermaid 11.12.0 and 10.2.4, and the keyword list follows
   11.12.0's `gantt.jison`; a change to the escaper or the diagram's syntax is checked by
   rendering `internal/export/testdata/mermaid/hostile.mmd` in both. Those goldens change
   only through `go test ./internal/export -update`, with the diff read before committing.
10. **The HTML export has one writer:** render it only through `export.WriteHTML`, from
    the
   same `Envelope`. The page embeds that envelope only as `export.Write` encodes it, in
   its `coroner-data` script element, and inlines its viewer and the vendored
   vis-timeline, so it makes no network request. The Go shell is a `text/template` that
   interpolates no text a person typed: `viewer.js` puts every string from the export on
   the page through `textContent` or an element it builds, never `innerHTML` or an HTML
   string handed to vis-timeline, and links only `http` and `https` URLs. Its PNG export
   builds the SVG through `createElementNS` and `textContent` too, and a link's location
   hash only picks among the sources, tags and events the export holds; its search text
   and times reach the page only as form control values. The viewer's filters select what
   `store.EventFilter` selects, so a change to either's semantics changes both. A spec
   renders hostile text in the fields the viewer shows and fails when it reaches the
   markup or ends a script or style element early; another fails when `viewer.js` names an
   HTML sink, such as `innerHTML`, or a vis-timeline `title`, which it would parse as
   HTML; and another when the bundle's SHA-256 is not the one its README records. The
   page's layout is not a contract (`AGENTS.md` Rule 3); its goldens in
   `internal/export/testdata/html/`, with the inlined files elided, change only through
   `go test ./internal/export -update`. The bundle changes only by the steps in
   [its README](../../internal/export/html/vendor/README.md).
11. **The HTML viewer's colours live in `theme.go`:** every colour the page shows is a
    token in `internal/export/theme.go` with a role, background, text or line, and
    `WriteHTML` writes the tokens into the page as custom properties with their light,
    dark and print values, so the SVG export can take the same source palette. Sources
    take palette colours by first appearance among the exported events, and a source's
    name always shows beside its colour. `viewer.css` names no colour and reads spacing,
    type sizes and radii from its own `:root` tokens; line-heights stay raw numbers. Specs
    in `internal/export/theme_internal_test.go` fail when a token or source colour is not
    a lowercase `#rrggbb`, when a text token is under WCAG AA contrast (4.5:1) on a
    background token, or a source colour under 3:1 on one, in either theme; line tokens
    have no contrast floor. A spec in `internal/export/html_test.go` fails when
    `viewer.css` gives a paint declaration, one whose property names a colour, background,
    border, outline, shadow, fill, stroke or source, custom properties included, anything
    but `var(--…)` references, numbers, lengths, commas, `solid`, `dashed`, `dotted`,
    `none`, `transparent` or `inherit`.
12. **A cluster never hides a gap:** `clusterCriteria` in `viewer.js` keeps a cluster to
    points spanning less than 5 minutes, with no range in its row whose midpoint falls
    within that span, ends included. The 5 minutes are the terminal viewer's
    `gapThreshold`, so a change to one changes both.
