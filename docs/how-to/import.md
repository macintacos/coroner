# Import a colleague's export

*Audience: people who share timelines with colleagues and want their additions back.*

Your database never leaves your machine: an export is what you share. When a colleague
adds events or annotations to a timeline you exported, they export it again and send it
back, and `coroner import` brings their export into your database. Importing is safe to
repeat: records identical to ones you already have are left as they are. Nothing is
written unless every record can be imported, except that attachments whose files are
missing are left out and the rest still imports. A colleague without coroner can
[comment in the HTML page](comment-in-the-html-page.md) instead, and send the comments
back as a file you import the same way.

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

The result counts, for timelines, sources, events, annotations, attachments and links, how
many records would be inserted, how many are identical to what you have, and how many
conflict with it. A dry run does not create or change your database, and exits with the
code the real import would.

Then import it:

```sh
coroner import their-inc-4821/inc-4821.jsonl
```

stderr names the timelines the file's records belong to, then summarises what happened,
one line per record type:

```text
Imported into timeline `inc-4821`
Imported timelines: 0 inserted, 1 identical, 0 conflicting
Imported sources: 1 inserted, 2 identical, 0 conflicting
Imported events: 1 inserted, 8 identical, 0 conflicting
Imported annotations: 3 inserted, 2 identical, 0 conflicting
Imported attachments: 1 inserted, 1 identical, 0 conflicting
Imported links: 0 inserted, 0 identical, 0 conflicting
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

`replace` cannot overwrite an attachment or a link, which never change once added. Nor can
it move an event to another timeline, or an annotation to another timeline or event. For
those, the import stops with exit 4 and writes nothing. `--on-conflict` applies to the
whole import, so importing again with `skip` keeps your copy of every conflicting record,
not only those.

A different conflict is a timeline whose name you already use for another timeline. The
import exits 4 with the kind `timeline_exists`, and `--on-conflict` does not resolve it.
Rename yours with `coroner timeline edit <name> --name <new-name>`, then import again.

## Import several exports at once

Exports can be concatenated and imported from stdin:

```sh
cat inc-4821.jsonl inc-4822.jsonl | coroner import -
```

stdin has no directory beside it, so attachment files are found only if your database
already has them. If two of the exports disagree about one record, the import is refused.

## Limit an import to one timeline

`-t` makes sure a file only touches the timeline you expect:

```sh
coroner import their-inc-4821/inc-4821.jsonl -t inc-4821
```

`-t` takes the timeline's name or ID. The timeline must already exist, or the import exits
3 with the kind `timeline_not_found`. If any record in the file belongs to another
timeline, the import exits 2 with the kind `invalid_input` and writes nothing. Without
`-t`, the file decides which timelines it imports into, and neither `CORONER_TIMELINE` nor
`coroner timeline use` has any effect.

## What changes and what does not

- **IDs never change.** Records match across databases by ID, and sources by name.
- **Event numbers can.** An event's number, its `seq`, is local to your database. An
  imported event keeps its number unless your timeline has already reached it, in which
  case it gets the next free one. `coroner event view 12` can show a different event on
  your machine than on your colleague's; `coroner event view <id>` always shows the same
  one.
- **Attachments without their files are left out.** An attachment is imported only when
  its file is in the `attachments/` directory beside the imported file, or is already in
  your database. The rest are listed in the result's `missing_attachments`, and stderr
  says how many:

  ```text
  Left out 1 attachment whose files are neither beside the export nor in the database; import an export written with -o <directory>/ to bring them
  ```

  Everything else still imports, and the import exits 0. Ask for an export made with
  `-o <directory>/`, then import that directory's file again: the missing attachments come
  in, and every record you already imported is identical.

## When an import is refused

The import exits 2 with the kind `invalid_input`, and writes nothing, when the file:

- has a line that is not a valid export record;
- was written by a coroner whose export format has another major version, such as `2.0`;
- has a record that breaks one of coroner's rules, such as an event with blank text;
- has an event whose `occurred_offset` is more than a day (86400 seconds) from UTC, or
  whose `seq` is absurdly large;
- has one ID twice with different content, as two concatenated exports that disagree about
  a record do (an event's `seq` may differ);
- has two timelines with one name;
- refers to a timeline or event that is neither in the file nor in your database;
- has a record in a timeline other than the one `-t` names;
- has an attachment file whose content does not match its checksum.

stderr names the first few problems and how many more there are. Keys and record types
this coroner does not know, which a newer coroner may add, are ignored.

For every flag, see [`coroner import`](../reference/cli/coroner_import.md) in the CLI
reference, and for the records themselves, the
[export format](../reference/export-format.md).
