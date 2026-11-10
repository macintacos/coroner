# Diagnostic log

*Audience: anyone looking into what coroner's commands did on their machine, by hand or
from a script.*

coroner appends a line of JSON to its diagnostic log for what a command does once it opens
a database. Nothing sets the log's path, size, retention or level.

## Where it lives

The log sits beside the database a command opens:

- `coroner.log` beside a database named `coroner.db`, so the default database logs to
  `$XDG_DATA_HOME/coroner/coroner.log`;
- `<db>.log` beside a database with any other name, so `/data/incidents.db` logs to
  `/data/incidents.db.log`.

A rotated log is `<log>.1`, such as `coroner.log.1`. Both files are owner-only (0600).

`--db` naming something that is not a coroner database, such as a directory or a file
coroner did not create, writes no log. Two paths can still collide:

- a database named `coroner`, with no extension, beside `coroner.db` shares `coroner.log`
  with it;
- a database whose `<db>.log` is another program's file, such as `app` beside an existing
  `app.log`, appends to that file and, past 50MB, renames it to `app.log.1`, replacing any
  older `app.log.1`.

## Which commands write it

Every command that opens a database that exists writes to the log, and ends with one
`command finished` line. `version`, `agent skill`, shell completion and every `__internal`
command never do.

A command against a database that does not exist when it ends, such as a read or a
`--dry-run`, writes no log and creates no directory. A log coroner cannot open, rotate or
write is skipped, and never changes a command's output or exit code.

## When lines are written

A command holds its lines until it opens its database, then appends them, and every later
line as it is logged, one write per line, so two coroner processes never interleave
partial lines.

## Rotation

A command that opens the log and finds it over 50MB renames it to `<log>.1`, replacing any
older `.1`, and starts a new file, so at most two log files exist. Rotation is best
effort: two processes rotating at once may lose the older file, and a long-running process
such as the viewer keeps writing to the renamed `.1` until it exits.

## Levels

Info lines are always written. The `command finished` line is info, with `command`, `kind`
(an exit kind from [Exit codes](cli/coroner_help_exit-codes.md), or `ok`), `exit_code` and
`duration` in nanoseconds.

A command that migrates a database older than the build also writes info lines:
`database backed up`, with the backup's `path` and `size`, when the database already has a
schema; then `migration applied`, once per migration, with the `schema_version` it brings
the database to. A new database applies every migration, so its first command logs one
`migration applied` per migration.

After `event delete`, `annotation delete`, `attachment delete` or `timeline delete`
commits, coroner removes the blobs no attachment uses any more, writing `blob deleted` for
each, with its `sha256` and `size`.

Each file a command writes outside the database adds a `file written` line at info, with
its `path` and `size` in bytes. `timeline export -o` adds the export's `format` and
`counts`, how many records of each kind it holds; each attachment file, whether
`attachment export -o` writes it or an export copies it beside a Markdown or JSONL export,
adds its `attachment` id and `sha256`. Output to stdout writes no such line.

A write command also logs, at info, a line for each record it creates, edits or deletes,
naming it by id. These lines are `timeline created`, `timeline edited` and
`timeline deleted` with `timeline_id`, and `current timeline set` with `timeline_id` or
`current timeline cleared`. They are `event added`, `event edited` and `event deleted`
with `event` and `timeline_id`. They are `annotation added`, `annotation edited` and
`annotation deleted` with `annotation`, `timeline_id` and, on an event, `event`. They are
`attachment added`, with `attachment`, its `event` or `annotation`, `size` and `sha256`,
and `attachment deleted`. They are `link added` and `link deleted` with `link`, `kind`,
`from_event` and `to_event`. Finally there are `overlay created` with `overlay_id` and
`source edited` with `source_id`. An edit's line adds `fields`: the names of the fields it
changed, as the export format spells them, never their values. Under `--dry-run` each line
carries `dry_run`. A command that finds its record already there, as `--if-not-exists` and
`event add --id` can, logs no line for it.

`import` and `event add --from-file` log, for each record type, how many records they
insert, skip and replace, such as `timelines replaced`, `events inserted` and
`attachments skipped`, each with its `count`, and only when there are some. A record
already stored unchanged, a conflict `--on-conflict skip` keeps, and an attachment whose
content is missing count as skipped. An import a conflict blocks writes nothing and logs
none of these. Under `--debug` the same message is logged once per record, with its id
under `timeline_id`, `source_id`, `event`, `annotation`, `attachment` or `link`. Under
`--dry-run` every such line carries `dry_run`.

Under `--debug` or `CORONER_DEBUG`, debug lines are written too, such as:

- `command failed`, with `kind`, `exit_code`, `message`, `flags` (the names of the flags
  given, never their values) and, when the failure has a cause, `error`;
- `database path resolved`, with the `path` and `by`: `flag`, `env`, `file` (the config
  file's `db`) or `default`; `config file resolved`, with the `path` coroner reads the
  config file from, whether or not a file is there, and `by`: `flag`, `env` or `default`;
  and `default time zone resolved`, with the `zone` and `by`, `file` (`default_timezone`)
  or `default`;
- `timelines selected`, each time a command selects them, with `by`, the setting that
  selected them: `flag` for `-t`, `overlay` for `--overlay`, `name` for a timeline
  argument, `env` for `CORONER_TIMELINE` or `use` for `coroner timeline use`, and
  `timeline_ids`, their ids;
- `time input resolved`, once for each time typed into `--at`, `--ended`, `--since` or
  `--until`, with the `flag`, the `input` as typed, the `zone` it was read in, and the
  `instant` and `precision` it resolved to;
- `database opened`, each time a command opens a database, with its `path`, `created`
  (true when the command created it), and its `schema_version` after any migration;
- `transaction committed` or `transaction rolled back`, once per database transaction,
  reads included, with `read_only` and its `duration`; a rolled-back one also has
  `dry_run`, true when `--dry-run` undid it. These lines tell whether a write committed,
  not what it changed;
- `blob stored`, the first time an attachment's content is stored, with its `sha256` and
  `size`;
- `blobs collected`, once garbage collection after a delete finishes, with the `count` of
  blobs it removed, which may be 0;
- `records read`, once per run of a command that only reads (`annotation list`,
  `attachment list`, `attachment export`, `event list`, `event view`, `source list`,
  `status`, `tag list`, `timeline list`, `timeline export` and `timeline view`), with
  `counts`, how many records of each kind it read, such as `{"events":12}`; `filter`, what
  narrowed the events: the `sources` and `tags` given, `contains`, true when `--contains`
  was given (never its text), `order`, `desc` when `--order desc` reversed them, and
  `limit`; and `event` or `attachment`, the id of the record `-e` or the argument named;
- `timeline picked`, with the `timeline` picked in `timeline list`'s picker;
- `export decoded`, when `import` or `event add --from-file` reads an export, with its
  `path` (`-` for stdin), how many `lines` it held and the `format_versions` of its
  headers, in order;
- the viewer's `timelines reloaded`, each time it reads its timelines again, on `r` or a
  new filter, with `counts` and `duration`; `timelines not reloaded`, with `duration` and
  `error`, when that read fails; and `attachment image not shown`.

Under debug, every line also prints on stderr as text as it is logged; the viewer holds
its stderr lines until it closes. stdout and the JSON error envelope never change, and the
envelope stays the last line of stderr.

## Line shape

Each line is one JSON object, with `time`, `level`, `msg` and `command` first, then the
line's own keys:

```json
{"time":"2026-11-09T19:35:41.934803-05:00","level":"INFO","msg":"command finished","command":"coroner timeline create","kind":"ok","exit_code":0,"duration":8667667}
```

A group, such as `counts`, is a nested object; on stderr its keys print dotted, such as
`counts.events=12`.

The line shape is not a contract: it may change in any release
([STABILITY.md](https://github.com/macintacos/coroner/blob/main/STABILITY.md)).

## Reading it

`coroner __internal log` prints the log as text, oldest line first: `<log>.1`, then
`<log>`. Each line prints as `--debug` prints it on stderr, as `key=value` pairs, with its
time in the zone `--tz` selects, as `event list` prints times. Four flags filter it:

- `--level` keeps lines at that level and above;
- `--command` keeps lines from that command path and the commands under it, such as
  `event add`;
- `--since` keeps lines at or after a time, and `--until` lines before one, read as
  `event list` reads them.

A line that is not a JSON log line prints as it is, whatever the filters, so a damaged log
still shows. Control characters print escaped. With no log yet, the command says so on
stderr and exits 0.

```sh
coroner __internal log
coroner __internal log --level warn
coroner __internal log --command "event add" --since "2026-09-11T03:00Z" --until "2026-09-11T04:00Z"
```

The command is hidden from help, prints text only and has no follow mode. It may change or
disappear in any release
([STABILITY.md](https://github.com/macintacos/coroner/blob/main/STABILITY.md)).

Scripts read the JSON file itself. The failed commands in the default database's log, with
`$XDG_DATA_HOME` at its default:

```sh
jq -c 'select(.msg == "command finished" and .kind != "ok")' ~/.local/share/coroner/coroner.log
```
