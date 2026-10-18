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
   Would add to timeline `checkout-latency` at 2026-09-11T00:02:30.412Z (+00:00 local: 00:02:30)
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
   Added event #1 to timeline `checkout-latency` at 2026-09-11T00:02:30.412Z (+00:00 local: 00:02:30)
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
Added event #4 to timeline `checkout-latency` at 2026-09-10T23:58:12.123Z (+00:00 local: 23:58:12)
```

## Times with no zone

A time with no offset or zone, such as a syslog or log4j stamp, is read in the first zone
of:

1. `--tz`;
2. `default_timezone` in the config file;
3. your local time zone.

coroner does not remember a zone per source, so pass `--tz` for every line from a host
whose zone differs from your default, and check the zone in the time coroner echoes.
[How coroner reads the times you type](../reference/cli/coroner_help_time.md) lists every
time format coroner reads.
