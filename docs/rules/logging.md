# Logging rules

*Audience: coding agents whose change `AGENTS.md` routes here.*

coroner logs through `log/slog` to the diagnostic log beside the database, which
[the diagnostic log reference](../reference/diagnostic-log.md) describes for users. These
rules say what a line holds; a new or changed line also updates that page.

1. **Each level has one job:** debug is internal state, the steps a command took and the
   values it decided on; info is what a command did; warn is a problem coroner recovered
   from and carried on past; error is what needs someone's attention. A failure the user
   already sees on stderr is not an error line: `command finished` records its kind at
   info, and `command failed` its detail at debug.
2. **A line carries only these:** ids, kinds, counts, sizes, durations, flag names, typed
   time input, the instant and precision typed time input resolves to, timeline, tag and
   source names, time zone names, the setting that supplied a value or selected the
   timelines, and the paths of files coroner opens. Everything else stays out, including
   titles, event text, annotation bodies, attachment content and filenames, and error text
   that quotes any of these. Name a record by its id, or by its name when it has one.
   Overlay names and an event's `<timeline>#<seq>` ref, such as `inc#3`, are allowed too.
3. **A path is logged only when coroner opens it and it holds no record content:** the
   database, the backup `Open` makes before migrating, the config file (the path coroner
   reads it from, or looks at when no file is there), the diagnostic log, the blob
   directory, and a file the user named with `-o` or as an `import` file are logged in
   full. A stored attachment's filename is record content, so neither it, a path coroner
   builds from it, nor the `--attach` path whose base name it is, is logged; name the
   attachment by its `attachment` id, with its `size` and `sha256`. Only the error for an
   `--attach` file coroner cannot read quotes that path, since nothing was stored.
4. **Error text never quotes record content:** `message` and `error` carry an error's text
   as it is, so a `*cli.Error`'s message, and any error it wraps, names a record by its
   id, name or `<timeline>#<seq>` ref, never by its title, text, body or filename.
5. **Attributes use the shared names:** a line uses the name below for what it carries,
   never a synonym. A change that adds an attribute name adds it to this list in the same
   change.
   - `command`: the command path, such as `coroner event add`; every line carries it.
   - `kind`: an exit kind, or `ok`, on `command finished` and `command failed`; elsewhere
     the kind of the record the line names, such as an annotation's or a link's.
   - `exit_code`: the process exit code.
   - `duration`: how long a command or step took, as a `time.Duration`.
   - `flags`: the names of the flags given, never their values.
   - `message`: a `*cli.Error`'s message (rule 4).
   - `error`: the error a `*cli.Error` wraps, or a recovered problem's error (rule 4).
   - `event`: an event's id.
   - `annotation`: an annotation's id.
   - `timeline`: the name of the timeline the line is about.
   - `count`: how many records a step changed or read.
   - `dry_run`: true when `--dry-run` kept the command from changing anything.
   - `path`: the path of a file coroner opens or writes (rule 3).
   - `size`: a file's or blob's size in bytes.
   - `attachment`: an attachment's id.
   - `sha256`: an attachment's SHA-256.
   - `by`: which setting supplied a value or selected the timelines: `flag`, `env`, `file`
     (the config file), `default`, `use` (`coroner timeline use`), `name` (a timeline
     argument) or `overlay`.
   - `timeline_ids`: the ids of the timelines a command selected, in the order it selected
     them.
   - `zone`: a time zone's name: `local`, `UTC` or an IANA name such as
     `America/New_York`.
   - `flag`: the name of the one flag a line is about, spelled as `flags` spells it, such
     as `at`.
   - `input`: the text typed into a time flag.
   - `instant`: the instant typed time input resolved to.
   - `precision`: the precision typed time input resolved to, such as `minute`.
   - `created`: true when opening created the database file.
   - `schema_version`: a database's schema version, or the one a migration brings it to.
6. **A command logs the records it names, and counts the ones it sweeps:** it logs one
   info line per record it names, under that record's key from rule 5, so a line about an
   annotation carries both its `annotation` and its `event`. Records it changes as a side
   effect, such as an import's records or a deleted timeline's events, get one info line
   with their `count`, and their ids at debug.
7. **A file written outside the database is logged at info:** an export, an exported
   attachment, the backup made before migrating, or any other file a command writes, with
   its `path` and `size`.
8. **The pure packages never log:** `internal/timeline`, `internal/timeparse`,
   `internal/timetext` and `internal/graphics` import no logger. Their callers log the
   inputs they pass and the results they get back. A pure function returns a decision as
   data, such as which rule matched, only when a caller logs it.
9. **`internal/store` takes a logger; `internal/config` returns data:** one store call
   does many I/O steps its caller cannot see, such as migrating, staging blobs and waiting
   on a busy database, so `store.Open`, `store.OpenReadOnly` and
   `store.OpenWithoutBlobSync` take a `store.WithLogger` option, which `internal/cli`
   hands them from the command, and the store logs those steps itself. `internal/config`
   does its I/O through the functions its caller passes and decides little beyond which
   path and which source won, so it returns those decisions in its result and
   `internal/cli` logs them. Code that logs takes its `*slog.Logger` as a parameter, never
   `slog.Default()` or the package-level `slog` functions.
10. **The content guard enforces rule 2:**
    [`log_content_test.go`](../../internal/cli/log_content_test.go) runs a table of
    commands under `--debug`, failing ones included, against records whose titles, event
    text, annotation bodies, attachment content and filenames hold sentinel strings, and
    fails when a sentinel reaches the log. A change that adds a log line adds a row whose
    run reaches that line, and a failing row when the line is reached by a command that
    fails after it opens the database.
