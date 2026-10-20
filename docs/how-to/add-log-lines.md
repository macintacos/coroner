# Add log lines

*Audience: people who use coroner and want lines from MongoDB, syslog or application logs
on their timeline.*

coroner does not parse logs. A log line you care about becomes an event you write: its
text says what the line means, its time is the line's time, and the line itself is kept
word for word as evidence.

## Add a line as an event

1. Copy the line.

2. Find its time, and check what coroner makes of it with `--dry-run`, which writes
   nothing:

   ```console
   $ coroner event add "Primary steps down on db-2" --at 2026-09-10T20:02:30.412-04:00 --source db-2/mongod --dry-run
   Would add to timeline `checkout-latency` at 2026-09-11T00:02:30.412Z (+00:00 UTC: 00:02:30)
   Precision:   millisecond
   Source:      db-2/mongod
   Tags:        -
   URL:         -
   Author:      julian
   Text:        Primary steps down on db-2
   ```

3. Run the same command without `--dry-run`:

   ```console
   $ coroner event add "Primary steps down on db-2" --at 2026-09-10T20:02:30.412-04:00 --source db-2/mongod
   Added event #1 to timeline `checkout-latency` at 2026-09-11T00:02:30.412Z (+00:00 UTC: 00:02:30)
   ```

4. Pipe the copied line into an evidence annotation on the event:

   ```console
   $ pbpaste | coroner annotation add - -e 1 --kind evidence --source db-2/mongod
   Added evidence annotation 01a09e58-ff02-7d6d-9602-c1780769bc5c to event #1 in timeline `checkout-latency`
   ```

   On Linux, use `wl-paste` or `xclip -selection clipboard -o` in place of `pbpaste`.
   Reading the line from stdin keeps it exactly as it was, without shell quoting.

The sections below show where the time is in common log formats.

## MongoDB

MongoDB 4.4 and later log one JSON document per line, with the time in `t.$date`:

```json
{"t":{"$date":"2026-09-10T20:02:30.412-04:00"},"s":"I","c":"REPL","id":21358,"ctx":"ReplCoord-0","msg":"Replica set state transition","attr":{"newState":"SECONDARY","oldState":"PRIMARY"}}
```

Pass `t.$date` to `--at` as it is, as in the steps above. It carries milliseconds and its
own offset, and the offset decides the time whatever `--tz` says. To print it from the
copied line:

```sh
pbpaste | jq -r '.t["$date"]'
```

## syslog

A traditional syslog line starts with the month, day and time, with no year and no zone:

```text
Sep 10 23:58:12 web-2 nginx[812]: upstream timed out (110: Connection timed out) while reading response header from upstream
```

Pass the stamp with `--tz` naming the zone of the host that wrote it:

```console
$ coroner event add "nginx upstream timeouts on web-2" --at "Sep 10 23:58:12" --tz America/Chicago --source web-2/nginx
Added event #2 to timeline `checkout-latency` at 2026-09-11T04:58:12Z (-05:00 America/Chicago: 23:58:12)
```

coroner gives a yearless stamp the latest year that does not put it after the moment you
run the command. A day padded to two characters with a space reads the same as one
without.

## Java and Python application logs

log4j, Logback and Python's `logging` write the date and time with a comma before the
milliseconds, and no zone:

```text
2026-09-10 23:58:12,345 ERROR [http-nio-8080-exec-7] c.a.checkout.PaymentClient - Timeout calling payments
```

Pass the stamp with `--tz`:

```console
$ coroner event add "Payments client times out" --at "2026-09-10 23:58:12,345" --tz America/Chicago --source checkout-api
Added event #3 to timeline `checkout-latency` at 2026-09-11T04:58:12.345Z (-05:00 America/Chicago: 23:58:12)
```

## Unix epoch times

Structured loggers often write the time as a number since the Unix epoch:

```json
{"level":"error","ts":1789084692123,"msg":"giving up on payment retry"}
```

Pass the number as it is. Its digit count picks the unit: 10 digits are seconds, and may
carry a fraction, 13 are milliseconds, 16 microseconds and 19 nanoseconds. An epoch time
needs no `--tz`:

```console
$ coroner event add "Worker gives up on payment retry" --at 1789084692123 --source payments-worker
Added event #4 to timeline `checkout-latency` at 2026-09-10T23:58:12.123Z (+00:00 UTC: 23:58:12)
```

## Times with no zone

A time with no offset or zone, such as a syslog or log4j stamp, is read in the first zone
of:

1. `--tz`;
2. `default_timezone` in the config file;
3. UTC.

coroner does not remember a zone per source, so pass `--tz` for every line from a host
whose zone differs from your default, and check the zone in the time coroner echoes.
[How coroner reads the times you type](../reference/cli/coroner_help_time.md) lists every
time format coroner reads.

## Add many lines at once

When a script picks the lines out of a log, have it write each one as an event record, one
per line of a JSONL file, and add the file with one command. coroner checks every record
before it adds anything, so one bad record adds nothing, and it adds the whole file in one
transaction.

A record is an event as `coroner timeline export --format jsonl` writes it, with every key
present. Its `timeline_id` is the id of the timeline you add to, which
`coroner timeline create checkout-latency --if-not-exists -q` prints, and its times are
RFC 3339 in UTC with nine fraction digits:

```json
{"type":"event","id":"01a0a07c-0000-7000-8000-000000000101","timeline_id":"01a09e58-f9b1-7c3e-8a41-2f6d0c9b7e15","seq":null,"occurred_at":"2026-09-11T00:02:30.412000000Z","occurred_offset":null,"precision":"millisecond","approximate":false,"occurred_raw":null,"ended_at":null,"source":"db-2/mongod","tags":[],"text":"Primary steps down on db-2","url":null,"author":"julian","created_at":"2026-09-11T00:10:00.000000000Z","updated_at":"2026-09-11T00:10:00.000000000Z"}
```

Each record carries its own `id`, a lowercase UUIDv7 your script makes. coroner refuses a
record without one, since an id it made up would add the event again every time the file
is added. Leave `seq` null, and coroner numbers the events in the order of the file.
[The export format](../reference/export-format.md#event) describes every key.

1. Check the file with `--dry-run`, which adds nothing; add `--json` to see each event:

   ```console
   $ coroner event add -t checkout-latency --from-file events.jsonl --dry-run
   Would add 3 events to timeline `checkout-latency`
   ```

   Under `--json` the result holds `dry_run`, `timelines`, which names the timeline,
   `inserted`, the events it adds, and `identical`, those it already holds unchanged. A
   dry run's inserted events have a null `seq`, since coroner numbers them only as it adds
   them. Added again, a file lists every event under `identical`.

2. Run the same command without `--dry-run`:

   ```console
   $ coroner event add -t checkout-latency --from-file events.jsonl
   Added 3 events to timeline `checkout-latency`
   ```

   `--from-file -` reads the records from stdin instead. With `-q` it prints the id of
   every event in the file, whether it added it or already held it.

Adding the same file again adds nothing and exits 0, so a script that stopped halfway can
simply run the command again. A record whose id coroner already holds with other content,
such as edited text, stops the whole file: nothing is added, and the command exits 4.

To make a single `coroner event add` safe to run again, give the event an id with `--id`,
its time with `--at` and its author with `--author`:

```console
$ coroner event add "nginx upstream timeouts on web-2" --id 01a0a07c-0000-7000-8000-000000000201 --at 2026-09-10T23:58:12-05:00 --author julian --source web-2/nginx -q
01a0a07c-0000-7000-8000-000000000201
```

Running it again adds nothing and prints the same id. A retry must repeat every value as
it was, or it exits 4. A time left to default to now differs on every run, and an author
left to default to the config file or `USER` differs when someone else retries. coroner
keeps `--at` as you typed it, so repeat it exactly: the same instant written another way
differs too.
