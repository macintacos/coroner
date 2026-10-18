---
title: coroner help exit-codes
---

# Exit codes and the JSON error envelope

*Audience: anyone running coroner from a script or an agent.*

coroner exits with one of these codes. Each failure also has a kind, which the JSON error
envelope names.

| Code | Meaning | Kinds |
| --- | --- | --- |
| 0 | Success | |
| 1 | Unexpected error, such as a bug | `internal` |
| 2 | Usage error or invalid input; nothing was written | `usage`, `invalid_time`, `invalid_name`, `timeline_required`, `ambiguous_id`, `invalid_input` |
| 3 | Not found | `timeline_not_found`, `event_not_found`, `annotation_not_found`, `source_not_found`, `attachment_not_found` |
| 4 | Conflicts with what is already stored, or a file that is already there | `timeline_exists`, `source_exists`, `id_conflict`, `file_exists` |
| 5 | Database unavailable: busy, unreadable or too new | `db_busy`, `db_unreadable`, `db_too_new` |
| 130 | Interrupted | `interrupted` |

New exit codes and kinds will be added over time. If an unknown exit code is returned, it
MUST be treated as a command failure. A code's meaning never changes.

## The JSON error envelope

An error prints on stderr, followed by a hint that says how to fix it when there is one.
Under `--json`, `--format json` or `--format jsonl`, the last line of stderr is a JSON
error envelope:

```json
{"error":{"kind":"usage","message":"unknown flag: --no-such-flag","hint":"Run 'coroner --help' for usage."}}
```

Every key is always present, and `hint` is `null` when there is none. `kind` is stable;
`message` and `hint` are for people and may change, so branch on the exit code or `kind`,
never on the text.
