# Diagnostic log

*Audience: anyone looking into what coroner's commands did on their machine, by hand or
from a script.*

coroner appends a line of JSON to its diagnostic log for what each command does. Nothing
sets the log's path, size, retention or level.

## Where it lives

The log sits beside the database a command opens:

- `coroner.log` beside a database named `coroner.db`, so the default database logs to
  `$XDG_DATA_HOME/coroner/coroner.log`;
- `<db>.log` beside a database with any other name, so `/data/incidents.db` logs to
  `/data/incidents.db.log`.

A rotated log is `<log>.1`, such as `coroner.log.1`. Both files are owner-only (0600).

## Which commands write it

Every command that opens a database that exists writes to the log, and ends with one
`command finished` line. `version`, `agent skill` and shell completion never do.

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

Info lines are always written. Today that is the `command finished` line, with `command`,
`kind` (an exit kind from [Exit codes](cli/coroner_help_exit-codes.md), or `ok`),
`exit_code` and `duration` in nanoseconds.

Under `--debug` or `CORONER_DEBUG`, debug lines are written too:

- `command failed`, with `kind`, `exit_code`, `message`, `flags` (the names of the flags
  given, never their values) and, when the failure has a cause, `error`;
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

```sh
jq -c 'select(.kind != "ok")' ~/.local/share/coroner/coroner.log
```
