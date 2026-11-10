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

Under `--debug` or `CORONER_DEBUG`, debug lines are written too, such as:

- `command failed`, with `kind`, `exit_code`, `message`, `flags` (the names of the flags
  given, never their values) and, when the failure has a cause, `error`;
- `database path resolved` and `config file resolved`, with the `path` and `by`, the
  setting that supplied it: `flag`, `env`, `file` (the config file's `db`) or `default`;
  and `default time zone resolved`, with the `zone` and `by`, `file` (`default_timezone`)
  or `default`;
- `timelines selected`, with `by`, the setting that selected them: `flag` for `-t`,
  `overlay` for `--overlay`, `name` for a timeline argument, `env` for `CORONER_TIMELINE`
  or `use` for `coroner timeline use`, and `timelines`, their ids;
- `time input resolved`, once for each time typed into `--at`, `--ended`, `--since` or
  `--until`, with the `flag`, the `input` as typed, the `zone` it was read in, and the
  `instant` and `precision` it resolved to;
- the viewer's `attachment image not shown`.

Under debug, every line also prints on stderr as text as it is logged; the viewer holds
its stderr lines until it closes. stdout and the JSON error envelope never change, and the
envelope stays the last line of stderr.

## Line shape

Each line is one JSON object, with `time`, `level`, `msg` and `command` first, then the
line's own keys:

```json
{"time":"2026-11-09T19:35:41.934803-05:00","level":"INFO","msg":"command finished","command":"coroner timeline create","kind":"ok","exit_code":0,"duration":8667667}
```

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
