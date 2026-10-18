---
name: coroner
description: Builds root-cause investigation timelines with the coroner CLI. Records timestamped events from logs, chat and people, keeps log lines and messages as evidence and interpretation as comments on those events, and exports the timeline as Markdown, JSONL or a Mermaid diagram. Use when investigating an outage or incident, reconstructing what happened when, turning log lines or Slack messages into a chronology, writing a postmortem timeline, or when the user mentions coroner.
compatibility: Requires the coroner CLI on PATH (github.com/macintacos/coroner). coroner agent skill prints the skill that matches the installed coroner.
---

# coroner

*Audience: coding agents that record an investigation timeline with the coroner CLI.*

coroner keeps each investigation as a named timeline in a local SQLite database. An
**event** is something that happened, at a moment or over an interval, as a source
reported it. An **annotation** hangs raw evidence or a comment off an event. This skill
gives the rules for driving coroner from an agent, and a recipe for each step of an
investigation. It does not list every flag: this prints every command and flag on one
page.

```sh
coroner help reference
```

## Rules

1. **Read JSON, branch on the exit code.** Pass `--json`, or `-q` where you need only a
   new record's id, to every command except `timeline export`, whose `--format` names the
   document it writes. Parse stdout. Human messages and errors go to stderr, and under
   `--json` the last line of stderr is a JSON error envelope such as
   `{"error":{"kind":"usage","message":"…","hint":"…"}}`. Decide what happened from the
   exit code, never from message text:

   | Code | Meaning | What to do |
   | --- | --- | --- |
   | 0 | Success | Read stdout. |
   | 2 | Usage error or invalid input; nothing was written | Fix the command from the envelope's `hint`. |
   | 3 | Not found | Check the name or id with a `list` command. |
   | 4 | Conflicts with what is stored, or an `-o` path that is already there | Read the stored record, or pick another `-o` path; never delete either to make room. |
   | 5 | Database unavailable | `db_busy`: run the same command again, once or twice. Any other kind: stop and give the user the envelope's `hint`. |

   Treat any other non-zero code as a failure. `coroner help exit-codes` lists the error
   kinds under each code. When coroner exits 2 on a flag or command this skill taught,
   with `unknown flag` or `unknown command` in the message, the installed coroner is older
   than this skill: run `coroner agent skill` and follow the skill it prints, which
   matches the installed coroner.

2. **Name the timeline with `-t`, and events by full id.** Pass `-t <timeline>` to each
   command that acts on a timeline. Never run `coroner timeline use` and never rely on
   `CORONER_TIMELINE`: the user or another process can change what is current between two
   of your commands. Name events by the full `id` from JSON output, never by `seq`.
   `event view`, `edit`, `delete` and `link` find an event by id in any timeline and
   reject `-t` beside an id; `annotation add` and `annotation list` take `-t` with
   `-e <id>`.

3. **Send RFC 3339 times with an explicit offset,** such as
   `2026-09-10T20:02:30.412-04:00` or `2026-09-11T00:02:30.412Z`. Natural-language times
   (`yesterday 23:02`, `around 14:00`) are experimental and may change in any release, so
   never send them. Convert a log's own time format first
   ([log sources](references/log-sources.md)). Then check that the `occurred_at` coroner
   prints, always in UTC, is the instant you meant.

4. **Facts are events; raw material is annotations.** An event's text is your own short
   statement of what happened, not the message itself, and `--source` names the system,
   service or person that reported it, such as `db-2/mongod` or `slack`. Keep the log
   line, chat message or query output word for word as an annotation with
   `--kind evidence`. Put your reading of it in an annotation with `--kind comment`.
   coroner records what happened and when: hypotheses and proposed fixes are outside its
   scope, so leave them out of the timeline.

5. **Add events in bulk from a JSONL file.** When you have more than about five events to
   add, write one event record per line
   ([JSONL event records](references/jsonl-events.md)), run
   `event add --from-file … --dry-run --json`, check the result, then run it again without
   `--dry-run`. coroner checks every record before it writes any, and adds the file in one
   transaction.

6. **Make every write safe to retry.** Give each event its own `--id`, a lowercase UUIDv7,
   with an explicit `--at` and `--author`, and create timelines with `--if-not-exists`.
   Running the same command again then writes nothing and exits 0. An exit 4 on a retry
   means the id is stored with different content, so compare before changing anything.
   `annotation add` has no `--id`, so running it again adds a second copy; before retrying
   one whose result you did not see, look for it with
   `coroner annotation list -t … -e … --json`. Never delete a record in order to retry. If
   a retried `event delete` exits 3, the first attempt already deleted the event, so it is
   not a failure.

7. **Preview a write you are unsure of.** Every write takes `--dry-run`, which prints
   under `--json` what it would write and writes nothing.

8. **Build only on stable surfaces.** Natural-language time, `--group-by`, `import`'s
   flags and the JSON fields of `status` and `version` are experimental. Use them only
   when the user asks, and say they may change.

## Recipes

### Start a timeline

```sh
coroner timeline create checkout-latency --title "Checkout p99 spike" --ref INC-4821 --if-not-exists --json
```

A name is at most 64 lowercase letters, digits, `.`, `_` or `-`; free text goes in
`--title`, and each ticket key or URL in its own `--ref`. The printed `id` is the
`timeline_id` a JSONL record needs, and `-q` in place of `--json` prints only that id.

### Add one event

```sh
coroner event add "Primary steps down on db-2" -t checkout-latency \
  --at 2026-09-10T20:02:30.412-04:00 --source db-2/mongod --tag phase:detect \
  --id 01a0a07c-0000-7000-8000-000000000101 --author agent --json
```

Tags are lowercase keywords, repeated once each. `--ended <time>` records an interval, and
`--url` a permalink to the log line or message. This makes a UUIDv7 with any `python3`:

```sh
python3 -c 'import os, time; b = bytearray(int(time.time() * 1000).to_bytes(6, "big") + os.urandom(10)); b[6] = b[6] & 15 | 112; b[8] = b[8] & 63 | 128; h = b.hex(); print(f"{h[:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:]}")'
```

The ids in this skill are placeholders: make a new one for every event, and never reuse
one or number ids by hand.

### Add events from a log

Pick the lines out of the log, write each as an event record in `events.jsonl`, then:

```sh
coroner event add -t checkout-latency --from-file events.jsonl --dry-run --json
coroner event add -t checkout-latency --from-file events.jsonl --json
```

The dry run's `inserted` lists the events the file would add, and `identical` those
already stored unchanged. A record that is invalid, or whose id is stored with other
content, adds nothing from the file and exits 2 or 4.

### Annotate an event

Pipe evidence in on stdin with `-`, so it is kept exactly, without shell quoting; coroner
drops one trailing newline. Write a comment as the body argument:

```sh
sed -n '1841p' mongod.log | coroner annotation add - -t checkout-latency \
  -e 01a0a07c-0000-7000-8000-000000000101 --kind evidence --source db-2/mongod --json
coroner annotation add "Step-down lands 4 s after the index build starts" -t checkout-latency \
  -e 01a0a07c-0000-7000-8000-000000000101 --kind comment --json
```

Without `-e`, an annotation belongs to the whole timeline. `--url` links the evidence to
where it came from.

### Review the timeline

```sh
coroner event list -t checkout-latency --json
coroner event list -t checkout-latency --since 2026-09-11T00:00:00Z --until 2026-09-11T01:00:00Z \
  --source db-2/mongod --tag phase:detect --limit 50 --json
coroner event view 01a0a07c-0000-7000-8000-000000000101 --json
coroner annotation list -t checkout-latency -e 01a0a07c-0000-7000-8000-000000000101 --json
```

Events come back in the order they happened. `--since` is inclusive and `--until`
exclusive; repeated `--source` flags match any of them, and repeated `--tag` flags must
all match. `--contains` matches text, ignoring case. When `--limit` cut the list,
`truncated` is `true` and `total` counts every match.

### Share the timeline

```sh
coroner timeline export -t checkout-latency --format md -o rca/
coroner timeline export -t checkout-latency --format jsonl -o checkout-latency.jsonl
coroner timeline export -t checkout-latency --format mermaid -o checkout-latency.mmd
```

`md` is an outline for people to paste into a ticket or postmortem. `jsonl` is the machine
format, which another coroner reads back with `coroner import`. For `md` and `jsonl`, a
directory `-o` such as `rca/` writes `<timeline>.md` or `<timeline>.jsonl` into it, with
each attachment's file under `attachments/`. `mermaid` is a gantt diagram, one section per
source. Filter flags such as `--since` and `--source` mean what they do on `event list`.
Only the JSON and JSONL shapes are contracts; the Markdown and Mermaid layouts may change.
