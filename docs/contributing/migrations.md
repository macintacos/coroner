# Migration policy

*Audience: maintainers and contributors, human or agent, who change coroner's database
schema or cut a release.*

coroner keeps a user's timelines in one local SQLite file, which may have been written
years earlier by an older coroner. The file never leaves that machine, so nobody else
would notice a broken upgrade path. These rules keep every database a release wrote
opening in every later release, and the fixture databases turn any break in that into a
failing test.

## The rules
Throwaway   paragraph with trailing spaces.   

1. **A released migration is never edited.** Once a release tag contains a migration in
   `internal/store/migrations/`, never edit or renumber it; change the schema by adding
   the next numbered file. Before the first tag, `00001_initial_schema.sql` can still
   change.
2. **Migrations only go forward.** coroner never migrates down. Migrations have no
   `-- +goose Down` block, and no user is ever told to roll one back.
3. **A newer database is refused.** When a database's schema version is higher than the
   highest migration the build embeds, `store.Open` leaves the file untouched and fails
   with `ErrDatabaseTooNew`: "this database was written by a newer coroner (schema N);
   upgrade coroner". The CLI exits 5 as `db_too_new`. An older build writing to a schema
   it does not know could damage it.
4. **A database is backed up before it is upgraded.** Before applying pending migrations
   to a database that already has a schema, `store.Open` copies it with `VACUUM INTO` to
   `<db>.pre-v<N>.bak`, where N is the newest schema the build embeds: one backup, however
   many migrations are pending. It never overwrites a backup: when that file exists, it
   fails with `ErrBackupExists` and names the file to move aside. A new database gets no
   backup.
5. **Every release commits a fixture database.** Each release, even one with no new
   migration, adds `internal/store/testdata/compat/v<X.Y.Z>.db`, written by that release,
   and every later build must open, migrate and read it
   ([§ Fixture databases](#fixture-databases)).
6. **The schema is internal, never a contract.** JSON and JSONL exports are the sharing
   contract ([`AGENTS.md`](../../AGENTS.md) Rule 3). The schema can change freely through
   new migrations, as long as every committed fixture still opens.

## Fixture databases

`go run ./internal/tools/compatfixture -o <path>` writes a new database through the store
API, never raw SQL, so it holds what that release really writes. It refuses a path that
already exists, so it can never touch a committed fixture or a real database. It writes:

- two timelines, one with several refs, and one of them current;
- events at every precision, including an approximate one and an interval, with offsets
  that are nil, zero and non-zero;
- events with and without a source, and with several tags;
- evidence and comment annotations on events, and one annotation on the timeline itself.

The compatibility spec in
[`internal/store/compat_test.go`](../../internal/store/compat_test.go), which
`mise run test` runs, checks a database the current build writes, because that is what the
next release commits, and every committed `testdata/compat/v*.db`. It copies each one into
a temporary directory and opens the copy with `store.Open`, which migrates an older
fixture after backing it up. It then compares every timeline, source, event and annotation
it reads back with the records compatfixture writes, field for field, and adds an event to
check that the sequence counter survived. The committed file is never opened in place.

A fixture records what a release wrote, so it is never edited, regenerated or deleted.
When a new migration breaks a committed fixture, fix the migration. compatfixture's
records are frozen too: every committed fixture holds them, and the spec compares against
them. A release that adds records must also teach the spec which fixtures hold them,
because older ones do not.

### Adding a release's fixture

The release checklist that EXC-1304 adds will carry these steps, run once the release is
tagged:

1. From a checkout of the new tag, such as one made by
   `git worktree add /tmp/coroner-v<X.Y.Z> v<X.Y.Z>`, run
   `go run ./internal/tools/compatfixture -o /tmp/v<X.Y.Z>.db`.
2. In a checkout of `main`, move the file to `internal/store/testdata/compat/v<X.Y.Z>.db`.
   `.gitattributes` already marks `*.db` as binary.
3. With the first fixture, `v0.1.0.db`, also add a spec to `internal/store/compat_test.go`
   that expects `testdata/compat/` to hold at least one fixture, so an empty directory
   fails the suite from then on. Later releases skip this step.
4. Still on `main`, run `mise run test`, then commit.

## Deferred gaps

- **No migration lock.** goose ships lockers for Postgres and MySQL, but not for SQLite.
  Two coroner processes that open the same database while a migration is pending,
  including a brand-new file, can both run it. One of them then fails, with a migration
  error such as "table timeline already exists", with `ErrBusy`, or with `ErrBackupExists`
  on the backup the other just took. Nothing is lost: the other process finishes the
  migration, and the failed command succeeds when run again. The fix is to hold
  `syscall.Flock` on `<db>.lock` around the version check, the backup and `Up`, and to
  check for pending migrations again once the lock is held. While `00001` is the only
  migration, no existing database can have one pending, so only the first commands on a
  brand-new database can race; the fix waits for the second migration, `00002_attachments`
  (EXC-1296).
- **Migrations run with foreign keys on.** A migration that rebuilds a table other tables
  reference relies on [store rule 3](../agents/store-rules.md) to turn foreign keys off;
  one that forgets it deletes every child row through the cascade. Running goose on a
  connection opened without `foreign_keys(1)`, then `PRAGMA foreign_key_check`, would rule
  that out. No migration rebuilds a table yet, so this waits for the same migration.
