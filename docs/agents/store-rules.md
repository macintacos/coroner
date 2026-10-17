# Store rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **A released migration is never edited:** once a release tag contains a migration,
   change the schema by adding the next numbered file under `internal/store/migrations/`.
   coroner never migrates down, so migrations have no `-- +goose Down`.
2. **A statement with a `;` before its end, such as a trigger, sits between
   `-- +goose StatementBegin` and `-- +goose StatementEnd`:** goose splits a migration on
   `;`, so the statement breaks without them.
3. **The CHECK constraints mirror `internal/timeline`:** the timeline name, source and tag
   grammars, the canonical instant, the precisions, the annotation kinds, the not-blank
   checks on ref, text and body, and ended at not before occurred at. Change the Go and
   the SQL in the same change. Only the table enforces that `id` is 36 characters,
   `seq > 0`, `occurred_offset` is within ±86400 seconds and `approximate` is 0 or 1; no
   Go rule mirrors them. SQLite cannot alter a CHECK, so changing one, even to add a
   precision or an annotation kind, needs a migration that rebuilds the table
   ([SQLite's procedure](https://www.sqlite.org/lang_altertable.html#otheralter)). A table
   another table references (`timeline`, `event`, `source`) is rebuilt in a migration
   marked `-- +goose NO TRANSACTION`, because inside goose's transaction
   `PRAGMA foreign_keys = OFF` is ignored and `DROP TABLE` cascades into every child row.
   Recreate the rebuilt table's indexes and triggers before `COMMIT`:

   ```sql
   -- +goose NO TRANSACTION
   -- +goose Up
   PRAGMA foreign_keys = OFF;
   BEGIN;
   CREATE TABLE event_new (…) STRICT;
   INSERT INTO event_new SELECT * FROM event;
   DROP TABLE event;
   ALTER TABLE event_new RENAME TO event;
   CREATE INDEX event_by_time ON event (timeline_id, occurred_at, seq);
   CREATE INDEX event_by_source ON event (source_id);
   COMMIT;
   PRAGMA foreign_keys = ON;
   ```

4. **sqlc generates static CRUD only:** a filter that varies with its input,
   `sqlc.narg(x) IS NULL OR …`, `sqlc.slice()`, `group_concat` and FTS5 are hand-written
   SQL, because sqlc's SQLite support mistypes or rejects them. Hand-written SQL lives in
   `internal/store`, outside `queries/`, where sqlc compiles every file.
5. **Generated models never become JSON:** `emit_json_tags` stays off in `sqlc.yaml`, and
   nothing marshals an `internal/store/sqlcgen` type to JSON, because JSON output is a
   contract (`AGENTS.md` Rule 3) and a generated model changes whenever the schema does.
