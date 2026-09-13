# coroner

_Audience: humans and coding agents naming things in code, flags, JSON and docs._

coroner reconstructs what happened during an investigation, in order, from evidence
scattered across systems, chat and people's memories.

## Language

### The record

**Timeline**: The named chronology of one investigation, usually tied to its refs.\
_Avoid_: incident, investigation, case

**Event**: Something that happened at a moment or over an interval, as a source reported
it. It is a claim about the past, even when it only comes from someone's memory.\
_Avoid_: entry, marker, note

**Annotation**: Material attached to an event, or to the whole timeline when it concerns
no single event. It has no place of its own in the chronology.\
_Avoid_: note

**Evidence**: An annotation that carries raw material verbatim, such as a log line or a
chat message.

**Comment**: An annotation that carries an investigator's commentary.

**Source**: The system, service or person an event or annotation came from, such as
`slack` or `db-2/mongod`.\
_Avoid_: process, origin, channel

**Tag**: A free lowercase keyword on an event. A colon marks a shared convention, as in
`phase:detect`.\
_Avoid_: label

**Ref**: An issue-tracker key, such as `OPS-42`, or a URL a timeline belongs to, kept as
written. A timeline can have many.\
_Avoid_: link, ticket

**Author**: The person who added an event or annotation, which is not necessarily its
source.\
_Avoid_: creator, owner, user

### Time

**Occurred at**: When an event happened, or when its interval started.\
_Avoid_: timestamp, time, event time

**Ended at**: When an interval ended. An event without one happened at a single moment.\
_Avoid_: end time, duration

**Created at**: When a record was first written into coroner. It never decides the order
of events.\
_Avoid_: recorded at, observed at

**Updated at**: When a record was last changed in coroner.

**Occurred raw**: When an event happened, exactly as it was given, before coroner resolved
it into an occurred at.\
_Avoid_: original time, input time

**Precision**: How exact an occurred at is, from a day down to a nanosecond. A coarse
event stands for the start of its window, so "10:15" comes before every event at 10:15 and
some seconds.\
_Avoid_: resolution, granularity

**Approximate**: Marks an occurred at as a guess, as in "around 10:15".\
_Avoid_: fuzzy, uncertain

**Gap**: A stretch of time between events that no event covers, often a blank the
investigation still has to fill.\
_Avoid_: hole, blank

### Finding things

**Seq**: The permanent per-timeline counter value an event gets when it is added, shown as
`#12`. It is never reused.\
_Avoid_: index, number, ID

**Current timeline**: The timeline `coroner timeline use` stores; a command acts on it
when neither `-t` nor `CORONER_TIMELINE` names one.\
_Avoid_: active timeline, context

**Overlay**: Several timelines read together as one chronology, without changing any of
them.\
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
