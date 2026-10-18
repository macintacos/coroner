# Query a timeline with jq

*Audience: people who use coroner and want to answer questions about a timeline with
[jq](https://jqlang.org/).*

A JSONL export holds one JSON record per line: the header, the timeline, the annotations
on the whole timeline, its sources, and its events, each followed by its attachments and
annotations. A record's `type` key names which it is, so each recipe below starts by
selecting the records it needs. The
[export format reference](../reference/export-format.md) lists every key and the record
order.

Export the current timeline to a file first:

```sh
coroner timeline export --format jsonl > rca.jsonl
```

The output shown for each recipe comes from running it on
[`full-model.jsonl`](https://github.com/macintacos/coroner/blob/main/testdata/export/v1.1/full-model.jsonl),
an export in coroner's repository, saved as `rca.jsonl`.

## List the events

```sh
jq -r 'select(.type == "event") | "#\(.seq) \(.occurred_at) \(.source // "-") \(.text)"' rca.jsonl
```

```text
#1 2026-09-11T00:00:00.000000000Z change-calendar Maintenance window opens
#2 2026-09-11T03:00:00.000000000Z cron Nightly backup starts
#3 2026-09-11T03:10:00.000000000Z grafana Oplog window drops under an hour
#8 2026-09-11T03:14:00.000000000Z - Replica set has no primary
#4 2026-09-11T03:14:07.000000000Z mongod Primary stepped down
#5 2026-09-11T03:14:07.412000000Z mongod Election timeout on db-3
#6 2026-09-11T03:14:07.412123000Z api Driver saw NotWritablePrimary
#7 2026-09-11T03:14:08.123456789Z mongod db-2 elected primary
```

Events are in the order they happened, so a seq can appear out of order.

## Count events by source

```sh
jq -c -s 'map(select(.type == "event")) | group_by(.source) | map({source: .[0].source, events: length})' rca.jsonl
```

```json
[{"source":null,"events":1},{"source":"api","events":1},{"source":"change-calendar","events":1},{"source":"cron","events":1},{"source":"grafana","events":1},{"source":"mongod","events":3}]
```

## Keep the events in a time window

Every time in an export is UTC with nine fractional digits, so times compare correctly as
strings:

```sh
jq -r 'select(.type == "event" and .occurred_at >= "2026-09-11T03:10" and .occurred_at < "2026-09-11T03:15") | "\(.occurred_at) \(.text)"' rca.jsonl
```

```text
2026-09-11T03:10:00.000000000Z Oplog window drops under an hour
2026-09-11T03:14:00.000000000Z Replica set has no primary
2026-09-11T03:14:07.000000000Z Primary stepped down
2026-09-11T03:14:07.412000000Z Election timeout on db-3
2026-09-11T03:14:07.412123000Z Driver saw NotWritablePrimary
2026-09-11T03:14:08.123456789Z db-2 elected primary
```

## Show each piece of evidence with its event

An annotation names its event by `event_id`, so index the events by `id` first. Evidence
on the whole timeline has a null `event_id` and prints as `(timeline)`:

```sh
jq -r -s 'INDEX(.[] | select(.type == "event"); .id) as $events | .[] | select(.type == "annotation" and .kind == "evidence") | "\(if .event_id then "#\($events[.event_id].seq) \($events[.event_id].text)" else "(timeline)" end): \(.body)"' rca.jsonl
```

```text
#8 Replica set has no primary: rs.status() showed no primary
```

## Find gaps between events

jq's `fromdateiso8601` rejects fractional seconds
([jqlang/jq#2224](https://github.com/jqlang/jq/issues/2224)), so strip the fraction before
converting a time to Unix seconds. A gap runs from the latest time an earlier event
covers, its `ended_at` or else its `occurred_at`, to the next event. This recipe prints
every gap of ten minutes or more:

```sh
jq -r -s 'def secs: sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601; map(select(.type == "event")) | foreach .[] as $e ({}; ($e.occurred_at | secs) as $at | .gap = $at - (.covered // $at) | .covered = ([.covered // $at, $at, ($e.ended_at // empty | secs)] | max); select(.gap >= 600) | "\(.gap / 60 | floor) min before #\($e.seq) \($e.text)")' rca.jsonl
```

```text
180 min before #2 Nightly backup starts
10 min before #3 Oplog window drops under an hour
```

## Query several exports at once

For timelines in your own database, name each with `-t`. One export holds them all, with
their events already merged in order:

```sh
coroner timeline export -t checkout-latency -t mongo-failover --format jsonl > overlay.jsonl
```

For exports someone sent you, JSONL exports concatenate: each one keeps its own `header`
and `timeline` records, and every event carries its `timeline_id`. Pipe several through
one recipe:

```sh
cat db.jsonl api.jsonl | jq -r 'select(.type == "event") | "\(.occurred_at) \(.text)"'
```

```text
2026-09-11T03:15:00.000000000Z Pager fires for db-2
2026-09-11T03:16:00.000000000Z Pager fires for api
```

This output comes from
[`concatenated.jsonl`](https://github.com/macintacos/coroner/blob/main/testdata/export/v1.1/concatenated.jsonl),
which is two exports concatenated. The events stay in file order; add `| sort` to order
them by time across the exports.
