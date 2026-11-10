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
   time input, timeline, tag and source names, and the paths of files coroner opens.
   Everything else stays out, including titles, event text, annotation bodies, attachment
   content and filenames, and error text that quotes any of these. Name a record by its
   id.
3. **A path is logged only when coroner opens it:** the database, the config file, the
   log, the blob directory, and a file the user named, such as `--attach`, `-o` or an
   `import` file, are logged in full. A stored attachment's filename is record content, so
   neither it nor a path coroner builds from it is logged; log the attachment's id.
4. **Error text never quotes record content:** `message` and `error` carry an error's text
   as it is, so an `Error`'s message, and any error it wraps, names a record by its id or
   name, never by its title, text, body or filename.
5. **Attributes use the shared names:** a line uses the name below for what it carries,
   never a synonym. A change that adds an attribute name adds it to this list in the same
   change.
   - `command`: the command path, such as `coroner event add`; every line carries it.
   - `kind`: an exit kind, or `ok`, on `command finished` and `command failed`; elsewhere
     the kind of the record the line names, such as an annotation's or a link's.
   - `exit_code`: the process exit code.
   - `duration`: how long a command or step took, as a `time.Duration`.
   - `flags`: the names of the flags given, never their values.
   - `message`: an `Error`'s message (rule 4).
   - `error`: the error an `Error` wraps, or a recovered problem's error (rule 4).
   - `id`: the id of the record the line names.
   - `timeline`: the name of the timeline the line is about.
   - `count`: how many records a step changed or read.
   - `dry_run`: true when `--dry-run` kept the command from changing anything.
   - `path`: the path of a file coroner opens or writes (rule 3).
   - `size`: a file's or blob's size in bytes.
   - `attachment`: an attachment's id.
   - `sha256`: an attachment's SHA-256.
6. **A command logs the records it names, and counts the ones it sweeps:** it logs one
   info line per record it names, with the record's `id`. Records it changes as a side
   effect, such as an import's records or a deleted timeline's events, get one info line
   with their `count`, and their ids at debug.
7. **A file written outside the database is logged at info:** an export, an exported
   attachment, or any other file a command writes, with its `path` and `size`.
8. **The pure packages never log:** `internal/timeline`, `internal/timeparse`,
   `internal/timetext` and `internal/graphics` import no logger. Their callers log the
   inputs they pass and the results they get back. A pure function returns a decision as
   data, such as which rule matched, only when a caller logs it.
9. **`internal/store` takes a logger; `internal/config` returns data:** one store call
   does many I/O steps its caller cannot see, such as migrating, staging blobs and waiting
   on a busy database, so the store is handed a `*slog.Logger` and logs those steps
   itself. `internal/config` does its I/O through the functions its caller passes and
   decides little beyond which path and which source won, so it returns those decisions in
   its result and `internal/cli` logs them. Code that logs takes its `*slog.Logger` as a
   parameter, never `slog.Default()` or the package-level `slog` functions.
10. **The content guard enforces rule 2:**
    [`log_content_test.go`](../../internal/cli/log_content_test.go) runs a table of
    commands under `--debug`, failing ones included, against records whose titles, event
    text, annotation bodies, attachment content and filenames hold sentinel strings, and
    fails when a sentinel reaches the log. A change that adds a log line to a command adds
    that command to the table, with a failing row when the command can fail after it opens
    the database.
