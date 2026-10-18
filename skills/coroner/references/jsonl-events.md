# JSONL event records

*Audience: coding agents writing a file for `coroner event add --from-file`.*

`event add --from-file` reads event records, one JSON object per line, in the shape
`coroner timeline export --format jsonl` writes them. It adds every record to the timeline
`-t` names, in one transaction, after checking them all.

## A record

Every key is present on every line, and each record sits on a single line:

```json
{"type":"event","id":"01a0a07c-0000-7000-8000-000000000201","timeline_id":"01a0a0dd-1b6a-7144-81c1-0e0a438eac89","seq":null,"occurred_at":"2026-09-11T04:58:12.000000000Z","occurred_offset":-18000,"precision":"second","approximate":false,"occurred_raw":"Sep 10 23:58:12","ended_at":null,"source":"web-2/nginx","tags":[],"text":"nginx upstream timeouts on web-2","url":null,"author":"agent","created_at":"2026-09-14T17:00:00.000000000Z","updated_at":"2026-09-14T17:00:00.000000000Z"}
{"type":"event","id":"01a0a07c-0000-7000-8000-000000000202","timeline_id":"01a0a0dd-1b6a-7144-81c1-0e0a438eac89","seq":null,"occurred_at":"2026-09-10T23:58:12.123456000Z","occurred_offset":null,"precision":"microsecond","approximate":false,"occurred_raw":"1789084692.123456","ended_at":null,"source":"slack","tags":["phase:detect"],"text":"On-call reports checkout p99 at 4s","url":"https://acme.slack.com/archives/C024BE91L/p1789084692123456","author":"agent","created_at":"2026-09-14T17:00:00.000000000Z","updated_at":"2026-09-14T17:00:00.000000000Z"}
```

| Key | What to write |
| --- | --- |
| `type` | `"event"`. |
| `id` | A lowercase UUIDv7 you make, unique per event. coroner refuses a record without one, since an id it made up would add the event again on every run. |
| `timeline_id` | The id of the `-t` timeline, which `coroner timeline create <name> --if-not-exists -q` prints. Every record names the same one. |
| `seq` | `null`. coroner numbers the events in file order. |
| `occurred_at` | When it happened, in UTC with nine fraction digits: `2026-09-11T04:58:12.000000000Z`. |
| `occurred_offset` | The offset of the RFC 3339 time you derived, in seconds east of UTC (`-18000` for `-05:00`). `null` only for a `Z` time or an epoch, as `event add --at` stores it. |
| `precision` | How exact the time is: `day`, `hour`, `minute`, `second`, `millisecond`, `microsecond` or `nanosecond`. Match the digits the source gave. |
| `approximate` | `true` only when the time is a guess. |
| `occurred_raw` | The time exactly as the source wrote it, such as `Sep 10 23:58:12` or a Slack `ts`, or `null` when you have none. |
| `ended_at` | `null`, or when an interval ended, in the same form as `occurred_at`. |
| `source` | The system, service or person that reported it, such as `db-2/mongod`, or `null`. |
| `tags` | An array of lowercase keywords such as `phase:detect`, or `[]`. |
| `text` | One short statement of what happened. Keep the raw line for an evidence annotation. |
| `url` | A permalink to the log line or message, or `null`. |
| `author` | Who is adding it, the same on every run. |
| `created_at`, `updated_at` | One instant, in the form of `occurred_at`, the same in both keys. Write it once and keep it identical on every run, so a rerun of the file matches what is stored. |

[The export format](https://macintacos.github.io/coroner/reference/export-format/#event)
describes every key.

## Add the file

```sh
coroner timeline create checkout-latency --if-not-exists -q
coroner event add -t checkout-latency --from-file events.jsonl --dry-run --json
coroner event add -t checkout-latency --from-file events.jsonl --json
```

The dry run prints `dry_run`, `timelines`, `inserted` (the events it would add, each with
a `null` `seq`) and `identical` (those already stored unchanged). Running the file again
adds nothing and exits 0, so a script that stopped halfway runs the same command again.
`--from-file -` reads the records from stdin, and `-q` prints the id of every event in the
file.

A record that is not valid exits 2, and one whose id is stored with other content, such as
edited text, exits 4. Either way nothing from the file is added. Fix the record, or read
the stored event with `coroner event view <id> --json` and decide which is right.

## Attach evidence after the file

The file carries events only. Once it is added, attach each raw line to its event by the
id you gave it:

```sh
sed -n '2207p' nginx-error.log | coroner annotation add - -t checkout-latency \
  -e 01a0a07c-0000-7000-8000-000000000201 --kind evidence --source web-2/nginx --json
```
