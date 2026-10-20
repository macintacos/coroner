# Timestamps and time zones

*Audience: people who want to understand how coroner stores, orders and shows times, and
why.*

An investigation's timeline is only as good as its order. Evidence arrives in every
format: log lines in UTC, chat messages in someone's local time, a memory of "around
10:15". coroner turns each into one instant it can sort, keeps enough of the original to
show what was given, and refuses to guess where a guess would reorder events. For the
times you can type and the flags that read them, see
[time input](../reference/cli/coroner_help_time.md).

## One fixed-width text instant

coroner stores every instant as text: RFC 3339 in UTC with exactly nine fractional digits,
such as `2026-09-11T03:14:07.412000000Z`. The same text appears in JSON output and
exports.

The fixed width is what makes the text sortable. Go's `time.RFC3339Nano` and many other
encoders trim trailing zeros, and a trimmed instant sorts wrong as a string: `…T10:00:00Z`
sorts after `…T10:00:00.1Z`, because `Z` comes after `.`. With nine digits every time,
sorting instants as strings sorts them in time, in SQLite, in `sort`, in jq and in any
script, without parsing a date.

## The offset is kept apart

UTC orders events, but it loses what the source said about local time. So an event also
keeps its occurred offset: the input's offset from UTC in seconds, as `+02:00` gives 7200.
An `--at` offset more than a day from UTC is rejected as `invalid_time`: real zones span
−12:00 to +14:00, so the database stores only offsets within a day of UTC.

The offset is null when the input was `Z`, `-00:00`, `-0000`, `UTC` or an epoch. Those say
when something happened but nothing about the local time where it happened, and
[RFC 9557](https://www.rfc-editor.org/rfc/rfc9557.html) gives `Z` exactly that meaning. A
stored zero would claim the source was in a UTC+0 zone, which none of them said.

The text as it was given is kept too, as the event's occurred raw, so an event's time can
always be checked against the evidence it came from.

## Precision and approximate times

A time is only as exact as what was given. `2026-09-11T03:14` is known to the minute, a
syslog line to the second, a database log to the millisecond. coroner stores that
precision with the event and sets its occurred at to the start of the window, measured in
the offset the input stated: `10:15` with minute precision is `10:15:00.000000000`.

So a coarse event sorts before every finer event inside its window. An event at `10:15`
comes before one at `10:15:30`, which is the safe reading: nothing known only to the
minute is placed after something that happened within that minute.

Approximate is separate from precision. `~10:15`, or `around 10:15`, marks a time as a
guess, such as a time recalled from memory, without claiming it is less exact. JSON keeps
both, and the Markdown, Mermaid, HTML and SVG exports all mark approximate times, so a
reader can tell a precise log line from a recollection.

## The order of events

Events are ordered by occurred at, then by timeline ID, then by seq. The timeline ID
breaks ties only between events of different timelines in an overlay, and seq, the order
events were added, breaks the rest. When a record was created never decides the order,
since evidence is often found long after it happened.

## Reading the zone of a typed time

A time without an offset is a wall-clock time, and coroner needs a zone to place it. It
takes the first of: the offset or zone in the input, `--tz`, `default_timezone` in the
[config file](../reference/configuration.md), then UTC. The input comes first because a
copied log line that states its offset is the most exact evidence there is. `--tz` beats
the config file because it describes this one input, and the config file only says where
you usually are. UTC comes last, rather than the zone of the machine coroner runs on, so
the same command reads the same time on a laptop, a server and CI; `local` asks for the
machine's zone by name.

A time read in UTC, whether UTC came from the default, `default_timezone` or `--tz`,
stores a null offset, like `Z`: reading it in UTC says nothing about the source's offset.
A named zone that sits at +00:00, such as `Atlantic/Reykjavik`, or a typed `+00:00`,
stores 0.

A wall-clock time that a daylight saving change skips or repeats in its zone is rejected,
with an error that names the zone and suggests the time written with the offset in force
before the change and with the one after it. `01:30` on the night clocks go back happens
twice; picking one would silently misorder the event by an hour, so coroner asks for the
offset instead.

A misread time is cheap to catch before it lands and costly once it has reordered a
timeline, so every command that writes a time echoes what it resolved, as
[time input](../reference/cli/coroner_help_time.md) describes.

## Showing times

Text output shows times in the zone of `--tz`, `default_timezone` or UTC, with the offset
written out. Exports are for other people in other zones, so the Markdown, Mermaid, HTML
and SVG exports show times in UTC unless `--tz` names a zone, and say which zone they
show. JSON is always UTC.

A coarse time's window does not always line up in the zone it is shown in. An hour
recorded at `+05:30` starts at half past the hour in UTC, so writing it as `04:--` there
would claim a window it was never given. Such a time is shown in the offset it was
recorded in instead, with its dashes kept: `10:-- +05:30`. The Markdown and SVG exports
and the terminal viewer still file it under the day its instant falls on in the shown
zone, so days stay in order, and they write its date beside it when that differs from the
day's heading: `2026-09-12 02:-- +05:30` under 2026-09-11 in UTC. An interval's end is
read where its start is, so a day recorded in UTC and shown in New York reads
`2026-09-11, until 2026-09-12` under 2026-09-10. The HTML export still shows such a time
in the shown zone, as `04:30`. Text output reads only a day-precision event in its own
offset: `event list --tz America/New_York` prints a day recorded in UTC as
`2026-09-11T00:00:00Z`.

## Clock skew is never corrected silently

Sources disagree about time: a server's clock drifts, a chat client shows when a message
arrived rather than when it was sent. Tracing tools have tried adjusting timestamps
automatically, and Jaeger's clock-skew adjuster was eventually
[considered harmful](https://github.com/jaegertracing/jaeger/issues/1459) for hiding real
data behind a guess.

coroner stores each time as its source reported it. When two sources disagree, both events
stay as reported and an annotation can say which clock was off and by how much. Any
correction coroner offers will be something you ask for and can see, never a change made
to times on the way in.

## Time zones per source are deferred

A source such as a server always logging in `America/New_York` could carry its own zone,
so its lines would need no `--tz`. That is deferred, not rejected: nothing in the data
model rules it out, and it waits until the need is clear. Until then, give such lines
their offset or pass `--tz` with them.
