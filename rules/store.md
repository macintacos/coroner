# Store rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

1. **A released migration is never edited:** once a release tag contains a migration,
   change the schema by adding the next numbered file under `internal/store/migrations/`.
   coroner never migrates down, so migrations have no `-- +goose Down`. Every release's
   fixture database must still open once the new migration applies
   ([the migration policy](../docs/contributing/migrations.md)).
2. **A statement with a `;` before its end, such as a trigger, sits between
   `-- +goose StatementBegin` and `-- +goose StatementEnd`:** goose splits a migration on
   `;`, so the statement breaks without them.
3. **The CHECK constraints mirror `internal/timeline`:** the timeline name, overlay name,
   source and tag grammars, the canonical instant, the precisions, the annotation and link
   kinds, the not-blank checks on ref, text and body, ended at not before occurred at, an
   attachment's single owner, sha256 and non-negative size, and a link's two different
   events. Change the Go and the SQL in the same change. `timeline.MaxOccurredOffset`
   mirrors the table's ±86400-second `occurred_offset` bound; only its checks that `id` is
   36 characters, `seq > 0` and `approximate` is 0 or 1 stay unmirrored, and `import`
   checks a `seq` headroom bound on JSONL input before writing. The Go rules `not_utf8`
   and `precision_start` have no CHECK, since SQLite cannot express them. Every `Tx` write
   method runs its record's `Validate` (`RenameSource` runs `timeline.ValidateSourceName`;
   an attachment gets only its owner's `Validate` and the filename check, since the SHA256
   and size are unknown before staging) before it writes, and returns its error unwrapped.
   A `Change` whose record is rejected discards its staged temporary files before it
   returns, and names no blob. SQLite cannot alter a CHECK, so changing one, even to add a
   precision or an annotation kind, needs a migration that rebuilds the table
   ([SQLite's procedure](https://www.sqlite.org/lang_altertable.html#otheralter)). A table
   another table references (`timeline`, `event`, `annotation`, `source`, `overlay`) is
   rebuilt in a migration marked `-- +goose NO TRANSACTION`, because inside goose's
   transaction `PRAGMA foreign_keys = OFF` is ignored and `DROP TABLE` cascades into every
   child row. Recreate the rebuilt table's indexes and triggers before `COMMIT`:

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
   `internal/store`, outside `queries/`, where sqlc compiles every file. The exception is
   `col = COALESCE(sqlc.narg(col), col)` in an `UPDATE` that keeps a column unless given
   one, as the `Overwrite*` statements keep `created_at` on an edit; every other parameter
   of that statement must then be `sqlc.arg`, because sqlc numbers a `narg` among bare
   `?`s so that SQLite binds it to the wrong argument.
5. **Generated models never become JSON:** `emit_json_tags` stays off in `sqlc.yaml`, and
   nothing marshals an `internal/store/sqlcgen` type to JSON, because JSON output is a
   contract (`AGENTS.md` Rule 3) and a generated model changes whenever the schema does.
   No exported store method takes or returns one either: they speak `internal/timeline`
   types. The `sqlcgen-private` depguard rule in [`.golangci.yml`](../.golangci.yml) fails
   any import of `sqlcgen` outside `internal/store`.
6. **A function passed to `Change` or `Read` uses only the `Tx` it is given:** calling a
   `Store` method there waits on the transaction's own write lock until `ErrBusy`. Every
   `Store` record method delegates to its `Tx` method. `Tx` methods translate their own
   errors, and a `Tx` write method runs in a savepoint (`Tx.atomic`), so one that fails
   leaves nothing behind; `fn` can branch on `ErrNotFound` or `ErrConflict` and keep
   going. A `Tx` method runs hand-written SQL (Rule 4) on `tx.db` through `queryAll`,
   since `sqlcgen.Queries` cannot run SQL it did not generate. A `Change` runs in one
   immediate transaction, which it retries whole, re-running `fn`, while another writer
   holds the database; so `fn` reads no `io.Reader`, prints nothing, and draws ids only
   through a source that replays them on a re-run. `Read` begins deferred and takes no
   write lock; `fn` passed to `Read` only reads. Every other transaction, garbage
   collection included, runs through `change`. Only `Open` and `OpenReadOnly` use the pool
   directly, for their identity and version checks, and `Open` also for its WAL pragma and
   backup. Never use `SetMaxOpenConns(1)` or `:memory:`: a query on the pool then waits
   forever while a transaction holds the only connection, and every pooled `:memory:`
   connection is a separate empty database.
7. **The store takes every ID and instant it stores from its caller:** callers pass every
   ID and instant, including the id a source gets when an event or annotation names it
   first. Only seq comes from the database.
8. **`Open` checks the file before it changes it:** `journal_mode = WAL` is set after the
   application-id check, never as a DSN pragma, which would rewrite a foreign file's
   header on connect. `OpenReadOnly` never sets it, and reads the schema version by hand,
   never through goose's `GetVersions`, which creates goose's table. When migrations are
   pending, `Open` holds a `flock` on `<db>.lock` from the WAL pragma through `Up`,
   because goose has no SQLite locker; never delete that file, or a waiter locks a file no
   later process opens.
9. **An attachment's blob is named before its row is written, and only garbage collection
   deletes a blob:** `Change` streams each `ChangeOptions.Attach` content into a synced
   temporary file in the blob directory before its transaction begins; `fn` names the
   owner with `Tx.Attach`, and only under `Commit`, after `fn` writes the owner's row,
   does `Change` rename each file to its SHA-256 and insert its row, so the owner and its
   attachments commit together and a failed owner write names no blob. `Change` also
   stages each `ChangeOptions.Blobs` content, with no owner, before its transaction
   begins; `Tx.Import` names those whose SHA-256 an attachment it inserts carries, only
   under `Commit`, after `fn`, does `Change` rename them, and the rest are discarded.
   `GarbageCollectBlobs` is never called inside a `Change`: it runs after a delete
   commits, in its own write transaction, so the write lock keeps it from deleting a blob
   between an add's rename and its insert. A crash leaves at most an unreferenced blob or
   a staged `.tmp-*` file, which garbage collection leaves, never a row without its blob.
   Nothing else deletes a blob, not `Open`, a delete or a migration. A `<db>.pre-v<N>.bak`
   backup copies the database file only: restored to the database's own path, it finds its
   blobs until `GarbageCollectBlobs` deletes those whose rows were removed.
10. **The blob directory comes from `config.BlobDir`, and a filename is never a path:**
    `Open` and `OpenReadOnly` derive the directory from the database path, so two
    databases never share one and one's garbage collection never deletes another's blobs.
    Blobs are keyed by hash alone, created 0600 in a 0700 directory; `Attachment.Filename`
    is display text.
11. **Rollback runs the change in full and undoes it; it names no blob:** a `Change` under
    `Rollback` is a dry run. It finds, validates and writes exactly as under `Commit`,
    then rolls the transaction back, so a dry run fails wherever its write would.
