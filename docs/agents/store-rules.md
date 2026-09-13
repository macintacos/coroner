# Store rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **A released migration is never edited:** once a release tag contains a migration,
   change the schema by adding the next numbered file under `internal/store/migrations/`.
   coroner never migrates down, so `-- +goose Down` is optional.
2. **A statement with a `;` before its end, such as a trigger, sits between
   `-- +goose StatementBegin` and `-- +goose StatementEnd`:** goose splits a migration on
   `;`, so the statement breaks without them.
3. **The CHECK constraints mirror `internal/timeline`:** the name, source and tag
   grammars, the canonical instant, the precisions and the annotation kinds. Change the Go
   and the SQL in the same change. SQLite cannot alter a CHECK, so changing one, even to
   add a precision or an annotation kind, needs a migration that rebuilds the table
   ([SQLite's procedure](https://www.sqlite.org/lang_altertable.html#otheralter)).
4. **sqlc generates static CRUD only:** a filter that varies with its input,
   `sqlc.narg(x) IS NULL OR …`, `sqlc.slice()`, `group_concat` and FTS5 are hand-written
   SQL, because sqlc's SQLite support mistypes or rejects them.
5. **Generated models never become JSON:** `emit_json_tags` stays off in `sqlc.yaml`, and
   nothing marshals an `internal/store/sqlcgen` type to JSON, because JSON output is a
   contract (`AGENTS.md` Rule 3) and a generated model changes whenever the schema does.
