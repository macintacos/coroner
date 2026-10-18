# Log sources

*Audience: coding agents turning log lines and chat messages into coroner events.*

`--at` also reads some log layouts, such as yearless syslog stamps, but only RFC 3339 is
stable (`coroner help time`). So you find each line's time, write it as RFC 3339 with an
explicit offset, and keep the line itself as evidence. A JSONL record takes UTC with nine
fraction digits and the offset in seconds ([JSONL event records](jsonl-events.md)). The
commands below print the forms each needs; they need Python 3.11 or later.

## Convert a time with an offset

```sh
python3 -c 'import sys, datetime as d; t = d.datetime.fromisoformat(sys.argv[1]); print(t.astimezone(d.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f000Z"), int(t.utcoffset().total_seconds()))' 2026-09-10T20:02:30.412-04:00
```

This prints `2026-09-11T00:02:30.412000000Z -14400`: the record's `occurred_at`, then its
`occurred_offset`. For a `Z` time, write `occurred_offset` as `null`.

## MongoDB JSON logs

MongoDB 4.4 and later log one JSON document per line, with the time in `t.$date`:

```json
{"t":{"$date":"2026-09-10T20:02:30.412-04:00"},"s":"I","c":"REPL","id":21358,"ctx":"ReplCoord-0","msg":"Replica set state transition","attr":{"newState":"SECONDARY","oldState":"PRIMARY"}}
```

`t.$date` is already RFC 3339 with milliseconds and an offset, so pass it to `--at` as it
is, with `precision` `millisecond` in a record. Read it, and the lines about one event,
with jq:

```sh
jq -r '.t["$date"]' mongod.log
jq -c 'select(.c == "REPL" and .msg == "Replica set state transition")' mongod.log
```

Use the host and process as the source, such as `db-2/mongod`, and the document itself as
the evidence annotation's body.

## syslog

A traditional (RFC 3164) syslog line starts with the month, day and time, with no year and
no zone:

```text
Sep 10 23:58:12 web-2 nginx[812]: upstream timed out (110: Connection timed out) while reading response header from upstream
```

Add the year and the zone of the host that wrote it, then print RFC 3339:

```sh
python3 -c 'import sys, datetime as d, zoneinfo as z; t = d.datetime.strptime(sys.argv[1], "%Y %b %d %H:%M:%S").replace(tzinfo=z.ZoneInfo(sys.argv[2])); print(t.isoformat())' "2026 Sep 10 23:58:12" America/Chicago
```

This prints `2026-09-10T23:58:12-05:00`. Never assume the host shares your zone: ask the
user, or read it from the host's configuration, and if you cannot learn it, say so rather
than guess. The year is the one the investigation happened in; for a line from late
December read in January, it is the year before. Use the hostname and program as the
source, such as `web-2/nginx`.

An RFC 5424 syslog line, or `journalctl -o short-iso-precise`, already carries an RFC 3339
time with an offset; pass it as it is.

## Slack messages

A message's `ts`, from the Slack API or a workspace export, is its posting time in seconds
since the Unix epoch with six fraction digits, such as `1789084692.123456`. A workspace
export holds one file per channel per day, `<channel>/<YYYY-MM-DD>.json`, an array of
messages with `ts`, `user` and `text`. Print `ts` as UTC:

```sh
python3 -c 'import sys, datetime as d; s, _, f = sys.argv[1].partition("."); print(d.datetime.fromtimestamp(int(s), d.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.") + f.ljust(9, "0") + "Z")' 1789084692.123456
```

This prints `2026-09-10T23:58:12.123456000Z`, exact to the microsecond, so write
`precision` `microsecond` in a record, or pass `2026-09-10T23:58:12.123456Z` to `--at`.
Never convert `ts` through a floating-point number, which can change its last digits.

A message's permalink carries the same time: in
`https://acme.slack.com/archives/C024BE91L/p1789084692123456`, the digits after `p` are
`ts` without its dot. Build the link from a channel id and `ts` the same way, and pass it
as the event's `--url`. Use `slack` as the source, and the message text, from `text`, as
the evidence annotation's body:

```sh
jq -r '.[] | select(.ts == "1789084692.123456") | .text' general/2026-09-10.json | coroner annotation add - -t checkout-latency \
  -e 01a0a07c-0000-7000-8000-000000000202 --kind evidence --source slack \
  --url https://acme.slack.com/archives/C024BE91L/p1789084692123456 --json
```
