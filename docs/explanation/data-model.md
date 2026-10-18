# Data model

*Audience: people who want to understand what coroner stores and why its records fit
together the way they do.*

coroner keeps investigations in one SQLite file: timelines, the events on them, the
annotations and attachments that back those events up, and the sources they came from.
This page explains how those records relate and the choices behind them, then gives the
glossary coroner uses for every name in its commands, JSON and docs.

## How the records relate

```mermaid
erDiagram
  TIMELINE ||--o{ EVENT : "is the home of"
  TIMELINE ||--o{ ANNOTATION : "holds"
  EVENT |o--o{ ANNOTATION : "is annotated by"
  EVENT |o--o{ ATTACHMENT : "keeps"
  ANNOTATION |o--o{ ATTACHMENT : "keeps"
  SOURCE |o--o{ EVENT : "reported"
  SOURCE |o--o{ ANNOTATION : "supplied"
  ATTACHMENT }o--|| BLOB : "stores its bytes in"
```

### A timeline contains events

Each event belongs to exactly one [timeline](#glossary), its home, because the claim an
event makes is part of the investigation that recorded it. Commands name a timeline by its
unique name, such as `mongo-failover`. To read several investigations side by side, such
as a database failover and the checkout latency it caused, you overlay their timelines. An
overlay is a query, not a record: it merges the timelines' events into one chronology and
changes none of them.

```sh
coroner event list -t mongo-failover -t checkout-latency
```

Because an overlay never copies or moves an event, an event cannot drift out of step with
a second copy of itself, and deleting one timeline never touches another.

### Annotations back events up

An [annotation](#glossary) is either on one event or on the whole timeline, and its kind
says what it holds: `evidence` or `comment`.

Evidence and commentary are one entity with a kind, rather than two, because they attach
the same way, list together under their event and export as one record type. The kind
keeps them apart where it matters: an evidence body is the source's words, and a comment
is yours.

Only events are placed on the chronology, so what happened stays separate from what
someone later found or thought about it.

### Sources, tags and refs

[Sources](#glossary) are global to the database rather than owned by a timeline, and
naming a new one in `--source` creates it. Each event or annotation names at most one
source. The same `db-2/mongod` then means the same thing on every timeline, which is what
lets `coroner source list` count its use and an import match sources across databases by
name.

[Tags](#glossary) are on events only, and carry no schema: agree on the conventions you
need, such as `phase:detect`, within your team.

A timeline's [refs](#glossary) are kept as text coroner never parses, so an `OPS-42` key
and a URL from another tracker sit side by side on one timeline.

### Attachments and blobs

An [attachment](#glossary) is kept with exactly one event or one annotation, never both.
Its bytes are addressed by their SHA-256 rather than by the attachment, so the same
screenshot attached twice costs one copy of the bytes. An export carries each attachment's
metadata and hash, and a Markdown or JSONL export written into a directory also copies the
files beside it.

### IDs and seqs

Every timeline, event, annotation and attachment has an ID, a UUIDv7: unique across every
database, so an import keeps each of their IDs and a colleague's export merges into yours
without two records sharing one. Sources are the exception: each database gives a source
its own ID, and an import matches sources by name. A UUIDv7 also starts with the time it
was made, so IDs made later sort later.

People do not type UUIDs, so each event also gets a [seq](#glossary), shown as `#12`. Seqs
follow the order events were added to their timeline, not when they happened: an event
backdated to last week still gets the next seq. The counter only increases, so a deleted
event's seq is never reused, and `#12` in last week's chat message still means the event
it meant then, or no event at all.

## Glossary

These are the names coroner uses in its commands, flags, JSON and docs. Most entries list
the words to avoid for the same idea. This glossary and the prior art below are the
repository's [`CONTEXT.md`](https://github.com/macintacos/coroner/blob/main/CONTEXT.md)
from its Language section on, word for word.

### The record

**Timeline**: The named chronology of one investigation, usually tied to its refs.

_Avoid_: incident, investigation, case

**Event**: Something that happened at a moment or over an interval, as a source reported
it. It is a claim about the past, even when it only comes from someone's memory.

_Avoid_: entry, marker, note

**Annotation**: Material attached to an event, or to the whole timeline when it concerns
no single event. It has no place of its own in the chronology.

_Avoid_: note

**Evidence**: An annotation that carries raw material verbatim, such as a log line or a
chat message.

**Comment**: An annotation that carries an investigator's commentary.

**Attachment**: A file, such as a screenshot, kept with one event or annotation. Identical
files attached twice are two attachments sharing one blob.

_Avoid_: upload, image

**Blob**: The bytes of an attachment, stored once under their SHA-256 in the blob
directory beside the database.

_Avoid_: object, asset

**Source**: The system, service or person an event or annotation came from, such as
`slack` or `db-2/mongod`.

_Avoid_: process, origin, channel

**Tag**: A free lowercase keyword on an event. A colon marks a shared convention, as in
`phase:detect`.

_Avoid_: label

**Ref**: An issue-tracker key, such as `OPS-42`, or a URL a timeline belongs to, kept as
written. A timeline can have many.

_Avoid_: link, ticket

**Author**: The person who added an event or annotation, which is not necessarily its
source.

_Avoid_: creator, owner, user

### Time

**Occurred at**: When an event happened, or when its interval started.

_Avoid_: timestamp, time, event time

**Ended at**: When an interval ended. An event without one happened at a single moment.

_Avoid_: end time, duration

**Created at**: When a record was first written into coroner. It never decides the order
of events.

_Avoid_: recorded at, observed at

**Updated at**: When a record was last changed in coroner.

**Occurred raw**: When an event happened, exactly as it was given, before coroner resolved
it into an occurred at.

_Avoid_: original time, input time

**Precision**: How exact an occurred at is, from a day down to a nanosecond. A coarse
event stands for the start of its window, so "10:15" comes before every event at 10:15 and
some seconds.

_Avoid_: resolution, granularity

**Approximate**: Marks an occurred at as a guess, as in "around 10:15".

_Avoid_: fuzzy, uncertain

**Gap**: A stretch of time between events that no event covers, often one the
investigation still has to explain.

_Avoid_: hole, blank

### Finding things

**Seq**: The permanent per-timeline counter value an event gets when it is added, shown as
`#12`. It is never reused.

_Avoid_: index, number, ID

**Current timeline**: The timeline `coroner timeline use` stores; a command acts on it
when neither `-t` nor `CORONER_TIMELINE` names one.

_Avoid_: active timeline, context

**Overlay**: Several timelines read together as one chronology, without changing any of
them.

_Avoid_: merge, combine

## Prior art

- Grafana calls the time-placed item itself an annotation. In coroner that item is an
  event, so the two meanings cannot collide.
- lnav uses annotation for output that scripts generate. A coroner annotation is added by
  a person or an agent.
- taskwarrior's annotations are timestamped notes attached to a task: the same shape as
  coroner's, and the reason annotation was kept over note.
- In AWS incident timelines, `eventType: Note` is itself a timeline item. A coroner
  annotation never sits on the time axis by itself.
- Source replaces process, the word in coroner's original brief, which suggested an
  operating-system process.
- Timeline was chosen over incident (not every investigation is an incident, and one
  incident can need several timelines), investigation (heavier) and case (a ticketing or
  legal connotation).
