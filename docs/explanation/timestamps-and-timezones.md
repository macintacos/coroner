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

The offset is null when the input was `Z`, `-00:00`, `UTC` or an epoch. Those say when
something happened but nothing about the local time where it happened, and
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
both, and the Markdown, Mermaid and SVG exports mark approximate times, so a reader can
tell a precise log line from a recollection.

## The order of events

Events are ordered by occurred at, then by timeline, then by seq. The timeline breaks ties
only between events of different timelines in an overlay, and seq, the order events were
added, breaks the rest. When a record was created never decides the order, since evidence
is often found long after it happened.

## Reading the zone of a typed time

A time without an offset is a wall-clock time, and coroner needs a zone to place it. It
takes the first of: the offset or zone in the input, `--tz`, `default_timezone` in the
[config file](../reference/configuration.md), then the local zone. The input comes first
because a copied log line that states its offset is the most exact evidence there is.
`--tz` beats the config file because it describes this one input, and the config file only
says where you usually are.

A wall-clock time that a daylight saving change skips or repeats in its zone is rejected,
with an error naming the zone and both offsets. `01:30` on the night clocks go back
happens twice; picking one would silently misorder the event by an hour, so coroner asks
for the offset instead.

Every command that writes a time echoes the instant it resolved, and `--dry-run` shows it
without writing, so a misread time is caught before it is on the timeline.

## Showing times

Text output shows times in the zone of `--tz`, `default_timezone` or the local zone, with
the offset written out. Exports are for other people in other zones, so the Markdown,
Mermaid, HTML and SVG exports show times in UTC unless `--tz` names a zone, and say which
zone they show. JSON is always UTC.

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
