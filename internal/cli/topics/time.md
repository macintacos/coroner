# How coroner reads the times you type

*Audience: anyone typing a time into coroner, by hand or from a script.*

Every command that takes a time, such as when an event occurred or `--since`, reads it the
same way.

## Time zone

coroner reads a time in the first zone of:

1. The offset or zone in the input, such as `Z`, `+02:00` or `UTC`.
2. `--tz`: an IANA zone such as `America/New_York`, or `local` for the zone `TZ` sets.
3. `default_timezone` in the config file, which takes the same values.
4. UTC.

Relative words and times of day, as in `yesterday 14:05`, are read in that zone too.

An `--at` offset more than a day from UTC, such as `+24:30`, is rejected as
`invalid_time`.

Text output, such as `coroner event list`, shows times in the zone of steps 2 to 4, with
its offset, and an event's occurred at to its precision, as in
`2026-09-12T10:05:03-04:00`. A day-precision event shows its own date, in the offset it
was recorded in.

## What you can type

- **RFC 3339, the stable grammar:** `2026-09-12T14:05:03Z` or
  `2026-09-12T14:05:03.250+02:00`. Scripts and agents send RFC 3339.
- **Log layouts:** `2026-09-12 14:05:03`, `2026-09-12T14:05`, either with an offset such
  as `-0700`, and yearless syslog stamps such as `Sep 12 14:05:03`, which take the latest
  year that is not in the future. A fraction of a second, after `.` or `,`, is kept.
- **Epochs,** optionally after `@`, whose digit count picks the unit: 10 digits are
  seconds and may carry a fraction, 13 are milliseconds, 16 microseconds and 19
  nanoseconds.
- A malformed date, time, offset or epoch in one of those forms, such as
  `2026-02-30T10:00:00Z` or `2026-09-12T14:05:03+5:30`, is rejected as `invalid_time`,
  never read as natural language.
- **Natural language, which is experimental:** `yesterday 14:05`, `3 hours ago` or
  `friday 9am`. It never resolves to the future, and what a phrase means may change when
  coroner's parser changes, so never script it.

## Approximate times

Start a time with `~` or `around` to mark it approximate, as in `~14:05` or
`around yesterday 14:05`.

## Daylight saving time

A wall-clock time that a daylight saving change skips or repeats in the zone is rejected,
never guessed. The error names the zone and both offsets; add the offset you mean.

## Precision

A time is as precise as what you typed: `2026-09-12T14:05` is precise to the minute. A
coarse time sorts at the start of its window, so `14:05` comes before every event at 14:05
and some seconds.

## Checking a time before it is written

Every command that writes a time echoes the time it resolved. `--dry-run` shows it without
writing anything.

## Ranges

`--since` is inclusive and `--until` is exclusive, so
`--since 2026-09-12T00:00:00Z --until 2026-09-13T00:00:00Z` covers exactly one day.

## Times in JSON

JSON output writes each instant as fixed-width RFC 3339 in UTC, with nine fractional
digits. jq's `fromdateiso8601` rejects the fraction
([jqlang/jq#2224](https://github.com/jqlang/jq/issues/2224)), so strip it first:

```sh
echo '"2026-09-12T14:05:03.000000000Z"' | jq 'sub("\\.[0-9]+Z$";"Z") | fromdateiso8601'
```
