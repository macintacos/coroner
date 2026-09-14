# Import a colleague's export

*Audience: people who share timelines with colleagues and want their additions back.*

Your database never leaves your machine: an export is what you share. When a colleague
adds events or annotations to a timeline you exported, they export it again and send it
back, and `coroner import` merges their export into your database. Importing is safe to
repeat: records you already have are skipped, and nothing is written unless the whole file
can be imported.

## Share a timeline with its attachment files

Export the timeline into a directory, so each attachment's file travels beside the JSONL:

```sh
coroner timeline export -t inc-4821 --format jsonl -o inc-4821/
```

This writes `inc-4821/inc-4821.jsonl` and `inc-4821/attachments/`. Send the whole
directory. A plain `--format jsonl` export to a file or stdout carries each attachment's
details and checksum, but not its file.

Your colleague imports it the same way you will import their copy:

```sh
coroner import inc-4821/inc-4821.jsonl
```

## Bring their changes back

When your colleague sends their export back, preview what it would change first:

```sh
coroner import their-inc-4821/inc-4821.jsonl --dry-run --json
```

The result counts, for timelines, sources, events, annotations and attachments, how many
records would be inserted, how many you already have unchanged, and how many conflict with
what you have. A dry run writes nothing, and exits with the code the real import would.

Then import it:

```sh
coroner import their-inc-4821/inc-4821.jsonl
```

stderr summarises what happened, one line per record type:

```text
Imported timelines: 0 inserted, 1 identical, 0 conflicting
Imported sources: 1 inserted, 2 identical, 0 conflicting
Imported events: 1 inserted, 8 identical, 0 conflicting
Imported annotations: 3 inserted, 2 identical, 0 conflicting
Imported attachments: 1 inserted, 1 identical, 0 conflicting
```

Every record keeps its `author`, `created_at` and `updated_at`, so you can still see who
added what, and when.

## Resolve conflicts

A conflict is a record you both have, with the same ID, whose content differs: an event
one of you edited, say. By default, any conflict stops the import: it exits 4 with the
kind `id_conflict`, and writes nothing. stderr lists each conflicting record and the keys
that differ, and with `--json` the result's `conflicts` list does too:

```json
{"type":"event","id":"0199a1b2-7c1e-7d40-8000-4f2a9c1e3b5d","fields":["text"]}
```

Choose which copy wins, and import again:

- `--on-conflict skip` keeps your records and imports everything else.
- `--on-conflict replace` overwrites your records with theirs.

`replace` cannot overwrite an attachment, which never changes once added, or move an event
or annotation to another timeline or event. The import stops with exit 4 instead; use
`skip` for those records.

A different conflict is a timeline whose name you already use for another timeline. The
import exits 4 with the kind `timeline_exists`. Rename yours with
`coroner timeline edit <name> --name <new-name>`, then import again.

## Import several exports at once

Exports can be concatenated and imported from stdin:

```sh
cat inc-4821.jsonl inc-4822.jsonl | coroner import -
```

stdin has no directory beside it, so attachment files are found only if your database
already has them.

## Limit an import to one timeline

`-t` makes sure a file only touches the timeline you expect:

```sh
coroner import their-inc-4821/inc-4821.jsonl -t inc-4821
```

The timeline must already exist, or the import exits 3. If any record in the file belongs
to another timeline, the import exits 2 and writes nothing. Without `-t`, the file decides
which timelines it imports into, and neither `CORONER_TIMELINE` nor `coroner timeline use`
has any effect.

## What changes and what does not

- **IDs never change.** Records match across databases by ID, and sources by name.
- **Event numbers can.** An event's number, its `seq`, is local to your database. An
  imported event keeps its number unless your timeline has already reached it, in which
  case it gets the next free one. `coroner event view 12` can show a different event on
  your machine than on your colleague's; the ID always names the same one.
- **Attachments without their files are skipped.** An attachment is imported only when its
  file is in the `attachments/` directory beside the imported file, or is already in your
  database. The rest are listed in the result's `missing_attachments` and counted on
  stderr; everything else still imports. Ask for an export made with `-o <directory>/`.

## When an import is refused

The import exits 2 with the kind `invalid_input`, and writes nothing, when the file:

- has a line that is not a valid export record;
- was written by a coroner whose export format has another major version, such as `2.0`;
- refers to a timeline or event that is neither in the file nor in your database;
- has an attachment file whose content does not match its checksum.

stderr names the first few problems and how many more there are. Keys and record types
this coroner does not know, which a newer coroner may add, are ignored.

For every flag, see [`coroner import`](../reference/cli/coroner_import.md) in the CLI
reference, and for the records themselves, the
[export format](../reference/export-format.md).
