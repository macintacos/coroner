# Export format

*Audience: anyone reading or writing coroner's JSON: people with jq, scripts, agents, and
importers.*

An export is coroner's only sharing contract. The SQLite database stays on the machine
that wrote it and its schema can change in any release; an export is what leaves. This
page describes the export format: its envelope, its objects, how they are encoded, and how
the format evolves. The commands that write exports are not in coroner yet (EXC-1316).

Command `--json` and JSONL output uses the same keys for a timeline, source, event, or
annotation today. A command may wrap them, as `event list`, `source list`, and `tag list`
do with `total` and `truncated`, or extend them with output-only keys, which this page
names beside the object they extend and which an export never holds. That output is
encoded as `coroner help formatting` describes; the [§ Encoding rules](#encoding-rules)
are what an export guarantees.

An *instant*, in the tables below, is a string in coroner's canonical time format: RFC
3339 in UTC with nine fractional digits, such as `2026-09-10T14:54:03.180000000Z`
([§ Encoding rules](#encoding-rules)). An *importer* is any program that reads exports.

This page is version **1.0** of the format. The
[JSON Schema](../../schema/export-v1.schema.json) describes the same objects and is
generated from the Go types in `internal/export`.

## The envelope

A JSON export is one object, the envelope. Its shape, with the record lists left empty:

```json
{
  "format": "coroner.export",
  "format_version": "1.0",
  "coroner_version": "0.1.0",
  "exported_at": "2026-09-13T01:30:00.000000000Z",
  "query": {
    "timelines": ["mongo-outage"],
    "since": null,
    "until": null,
    "sources": [],
    "tags": [],
    "contains": null,
    "limit": null
  },
  "timelines": [],
  "sources": [],
  "events": [],
  "annotations": []
}
```

| Key | Type | Meaning |
| --- | --- | --- |
| `format` | string | Always `coroner.export`. An identifier, not a URL. |
| `format_version` | string | `MAJOR.MINOR` version of this format, such as `1.0`. |
| `coroner_version` | string | Version of coroner that wrote the export. |
| `exported_at` | instant | When the export was written. |
| `query` | object | The selection the export was made with. |
| `timelines` | array of timeline | The exported timelines. |
| `sources` | array of source | The sources the exported events and annotations name. |
| `events` | array of event | The exported events, from every exported timeline. |
| `annotations` | array of annotation | The exported annotations. |

The record lists are flat. An event carries its `timeline_id` rather than sitting inside
its timeline, and an annotation carries its `event_id` rather than sitting inside its
event. Reading several timelines together is then a merge of their events on time, and an
export can hold any mix of timelines without changing shape.

`events` is ordered by `occurred_at`, then `seq`. The other lists have no defined order;
sort them yourself if you need one.

### query

`timelines` names the timelines exported. Every other key is a filter on events, and
`null` or an empty list means that filter was not applied.

| Key | Type | Meaning |
| --- | --- | --- |
| `timelines` | array of string | Names of the timelines exported. |
| `since` | instant or null | Kept events that occurred at or after this instant (inclusive). |
| `until` | instant or null | Kept events that occurred before this instant (exclusive). |
| `sources` | array of string | Kept events from **any** of these source names. |
| `tags` | array of string | Kept events carrying **all** of these tags. |
| `contains` | string or null | Kept events whose text contains it, ignoring ASCII case. |
| `limit` | integer or null | At most this many events were exported. |

## Records

These objects appear in an envelope's lists and as JSONL records. The vocabulary is
coroner's glossary, `CONTEXT.md`.

An event or annotation names its source by `name`, not by ID: to find its record in
`sources`, match its `source` to a source's `name`.

### Timeline

| Key | Type | Meaning |
| --- | --- | --- |
| `id` | string | The timeline's ID. |
| `name` | string | Unique name, such as `mongo-outage`. |
| `title` | string | Free-text heading; may be `""`. |
| `refs` | array of string | Issue-tracker keys or URLs the timeline belongs to, as written. |
| `created_at` | instant | When the timeline was created in coroner. |

`coroner timeline list` also prints a `current` key, which says whether the timeline is
the database's current timeline. That is state of one machine, so it is output only: it is
never part of an exported timeline.

### Source

| Key | Type | Meaning |
| --- | --- | --- |
| `id` | string | The source's ID. |
| `name` | string | Name, such as `slack` or `db-2/mongod`. |
| `created_at` | instant | When the source was first recorded in coroner. |

`coroner source list` and `source edit` also print `event_count`, `annotation_count`,
`first_occurred_at`, and `last_occurred_at`, which describe the source's use in one
database. They are output only: never part of an exported source.

### Event

| Key | Type | Meaning |
| --- | --- | --- |
| `id` | string | The event's ID. |
| `timeline_id` | string | ID of the event's timeline. |
| `seq` | integer or null | Permanent per-timeline counter, shown as `#12`; null only in a preview, see below. |
| `occurred_at` | instant | When it happened or its interval started: the start of its precision window. |
| `occurred_offset` | integer or null | The input's offset from UTC in seconds, positive east; null when the input was `Z` or an epoch, which say nothing about a local offset. |
| `precision` | string | How exact `occurred_at` is; see below. |
| `approximate` | boolean | Whether `occurred_at` is a guess. |
| `occurred_raw` | string or null | The time exactly as it was given; null when none was given and the event took the time it was added. |
| `ended_at` | instant or null | When the interval ended; null for a single moment. |
| `source` | string or null | Name of the source the event came from. |
| `tags` | array of string | Lowercase keywords, such as `phase:detect`. |
| `text` | string | What happened. |
| `url` | string or null | Permalink to the thing itself, such as a log line or chat message. |
| `author` | string or null | Who added the event, not necessarily its source. |
| `created_at` | instant | When the event was added to coroner. |
| `updated_at` | instant | When the event was last changed in coroner. |

Every stored event has a `seq`. It is `null` only in a preview of an event not yet added,
such as `coroner event add --dry-run`, so no event in an export has a `null` `seq`.

In format 1.0, `precision` is one of `day`, `hour`, `minute`, `second`, `millisecond`,
`microsecond`, or `nanosecond`. It is an open enum ([§ Encoding rules](#encoding-rules)):
the schema does not restrict its values, and a consumer accepts one it does not know. A
coarse event stands for the start of its window, so an event at `10:15` with precision
`minute` sorts before one at `10:15:30`.

### Annotation

| Key | Type | Meaning |
| --- | --- | --- |
| `id` | string | The annotation's ID. |
| `timeline_id` | string | ID of the annotation's timeline. |
| `event_id` | string or null | ID of the annotated event; null for the whole timeline. |
| `kind` | string | `evidence` for raw material, `comment` for commentary; an open enum. |
| `body` | string | The annotation's content. |
| `author` | string or null | Who added the annotation, not necessarily its source. |
| `source` | string or null | Name of the source the annotation came from. |
| `url` | string or null | Permalink to the thing itself. |
| `created_at` | instant | When the annotation was added to coroner. |
| `updated_at` | instant | When the annotation was last changed in coroner. |

### JSONL records

A JSONL export writes one record per line. A record is the object above with one extra key
first, `type`, naming the object: `timeline`, `source`, `event`, or `annotation`.

```json
{"type":"event","id":"01a09862-b1c0-7000-8000-000000000002","timeline_id":"01a09862-b1c0-7000-8000-000000000001","seq":1,"occurred_at":"2026-09-11T03:14:07.412000000Z","occurred_offset":null,"precision":"millisecond","approximate":false,"occurred_raw":"2026-09-11T03:14:07.412Z","ended_at":null,"source":"mongod","tags":["election"],"text":"Primary stepped down","url":null,"author":null,"created_at":"2026-09-13T01:30:00.000000000Z","updated_at":"2026-09-13T01:30:00.000000000Z"}
```

## Encoding rules

- **Keys are snake_case**, and appear in the order the tables above list them. Key order
  is not part of the contract: a consumer never depends on it, and a minor version may add
  a key at any position.
- **Every key is always present.** A missing scalar is `null`, and an empty collection is
  `[]` or `{}`; a key is never left out. A consumer can tell "unknown" from "not in this
  version" by the key's presence.
- **An instant is fixed-width RFC 3339 in UTC with nine fractional digits**, such as
  `2026-09-10T14:54:03.180000000Z`. At that width, sorting instants as strings sorts them
  in time. Go's `time.RFC3339Nano` trims trailing zeros, which breaks that: as strings,
  `…:03Z` sorts after `…:03.1Z`.
- **`precision` and `kind` are open enums.** A later minor version may add values, so
  treat a value you do not know as valid.
- **`source` is a name**, not an ID, so records match across databases by name.
- **`<`, `>`, and `&` are escaped** as `\u003c`, `\u003e`, and `\u0026`, so the JSON can
  sit inside an HTML `<script>` element. Every JSON parser decodes them back.
- **Output is deterministic:** the same values encode to the same bytes.
- **Strings are valid UTF-8.**

jq's `fromdateiso8601` rejects fractional seconds
([jqlang/jq#2224](https://github.com/jqlang/jq/issues/2224)). Strip them first; the result
is whole seconds since the Unix epoch, without the sub-second part:

```sh
jq '.events[] | .occurred_at | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601'
```

## Evolution rules

`format_version` is `MAJOR.MINOR`.

**A minor version** is backward compatible. It may add:

- a new key to an object, present like every other key in that version and ignored by
  older consumers;
- a new JSONL record `type`;
- a new value to an open enum.

Consumers ignore keys and record types they do not know, and accept enum values they do
not know.

**A major version** is anything else: removing or renaming a key, or changing a key's
type, meaning, unit, time encoding, or sort order.

An importer reads the major version as the integer before the dot. It rejects an export
whose `format` is not `coroner.export`, or whose major version it does not support, with
an error that names both versions.

Further rules:

- A removed key's name is never reused.
- A deprecated key stays for at least one major version, and the schema marks it
  `"deprecated": true`.
- A `--format-version` flag, to write an older format version, waits until 2.0.

## The schema

[`schema/export-v1.schema.json`](../../schema/export-v1.schema.json) is a JSON Schema,
draft 2020-12, with the `$id`
`https://raw.githubusercontent.com/macintacos/coroner/main/schema/export-v1.schema.json`.
It describes the envelope at its root and each record under `$defs`. It is open: it lists
every key as required but allows keys it does not list, so an export from a later minor
version still validates.

The schema is generated from the Go types by `go run ./internal/tools/schemagen` and is
never edited by hand. `mise run lint` fails when it is out of date.
