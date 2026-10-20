# Your first timeline

*Audience: people new to coroner who want to learn it by building one timeline.*

In this tutorial you build the timeline of a checkout latency incident. You add five
events, attach a piece of evidence and a comment, look at the result in the terminal, and
export it as Markdown you could paste into a ticket. It takes about ten minutes.

You need coroner installed; [Install coroner](../how-to/install.md) covers that. Run every
command in the same terminal, in a directory you can write to.

## Use a database of your own

coroner keeps every timeline in one SQLite file. So that this tutorial starts from nothing
and leaves your real timelines alone, point coroner at a new file for this terminal:

```sh
export CORONER_DB="$PWD/coroner-tutorial.db"
```

coroner creates `coroner-tutorial.db` in this directory the first time a command writes to
it, and keeps using it even if you change directory.

## Create a timeline

A timeline is the record of one investigation. Its name is how you refer to it in
commands; its title is free text; its refs are the ticket keys or URLs it belongs to.
Create one with two refs:

```console
$ coroner timeline create checkout-latency --title "Checkout latency spike" --ref INC-4821 --ref https://tracker.example.com/INC-4821
Created timeline `checkout-latency`.
Next: coroner timeline use checkout-latency
      coroner event add -t checkout-latency <text>
```

Make it the current timeline, so the commands that follow act on it without naming it:

```console
$ coroner timeline use checkout-latency
Now using timeline `checkout-latency`.
```

## Add events

An event is something that happened, with the time it happened and the source that told
you about it. Times you copy from a log or a dashboard are exact, so type them in RFC
3339, with `Z` for UTC. Add the three events the dashboards and logs show:

```console
$ coroner event add "Deploy 812 starts rolling out to checkout" --at 2026-09-10T23:40:00Z --source deploy-bot
Added event #1 to timeline `checkout-latency` at 2026-09-10T23:40:00Z
$ coroner event add "Checkout p99 latency alert fires" --at 2026-09-10T23:58:12Z --source grafana
Added event #2 to timeline `checkout-latency` at 2026-09-10T23:58:12Z
$ coroner event add "Replica set has no primary" --at 2026-09-11T00:02:30Z --ended 2026-09-11T00:05:40Z --source mongod
Added event #3 to timeline `checkout-latency` at 2026-09-11T00:02:30Z, ended 2026-09-11T00:05:40Z
```

Each line coroner prints is the time it stored, in UTC; set `--tz` or `default_timezone`
to also see it in your own zone. The third event lasted just over three minutes, so it has
an end as well as a start. `#1`, `#2` and `#3` are the events' seqs: the numbers you use
to refer to them later.

Times people tell you are rarely exact. Type those the way you would say them:

```console
$ coroner event add "Customer reports checkout is still slow" --at "yesterday 14:05" --source support
Added event #4 to timeline `checkout-latency` at 2026-09-13T14:05:00Z
$ coroner event add "Rolled back deploy 812" --at "3 hours ago" --source deploy-bot
Added event #5 to timeline `checkout-latency` at 2026-09-14T02:00:00Z
```

This page was run on 2026-09-14 at 05:11 UTC. "3 hours ago" is precise to the hour, so
coroner stored the start of that hour, which is why the echo shows 02:00:00 and not
02:11:00. Your dates for these two events are yesterday and three hours before you ran the
command, shown in UTC unless you set `--tz` or `default_timezone`. Check the echoed time
each time you type one like this: it is how you catch a phrase that meant something other
than you intended.

## Attach evidence and a comment

An annotation is material attached to an event. Evidence keeps raw material exactly as it
was, such as the alert's text; a comment holds what you think about it. Attach the alert
to event 2 as evidence:

```console
$ coroner annotation add "[FIRING:1] CheckoutP99High checkout p99 = 4.1s (threshold 3s)" -e 2 --kind evidence --source grafana
Added evidence annotation 01a09e53-fc11-7124-9494-32a4ee69ba27 to event #2 in timeline `checkout-latency`
```

Then add a comment to event 3:

```console
$ coroner annotation add "The election starts 4 minutes after the alert, so it is a symptom, not the trigger" -e 3 --kind comment
Added comment annotation 01a09e53-fc99-7d61-aa27-9b21cf27597b to event #3 in timeline `checkout-latency`
```

Your output will differ from this page in a few more places: the annotation IDs above, the
timeline's ID and creation time in the text view below, and the export time and coroner
version in the Markdown export.

## View the timeline

Open the timeline in the viewer:

```sh
coroner timeline view
```

The viewer lists the events in the order they happened, grouped by day, with a line
marking each silence of five minutes or more between events. Those are the stretches your
timeline does not explain yet. Move with the arrow keys, press `enter` on event 2 to see
its evidence, press `?` to list every key, and press `q` to quit. When an event's evidence
is taller than the screen, the arrow keys scroll through it a line at a time before moving
to the next event, and `pgup` and `pgdn` move to the first event off screen.

To print the same timeline as plain text instead, add `--format text`:

```console
$ coroner timeline view --format text
checkout-latency
ID:          01a09e53-f87a-7298-a557-f2094adcf6b1
Title:       Checkout latency spike
Refs:        INC-4821, https://tracker.example.com/INC-4821
Created:     2026-09-14T05:11:38Z
Events:      5, from 2026-09-10T23:40:00Z to 2026-09-14T02:00:00Z
Sources:     deploy-bot 2, grafana 1, mongod 1, support 1
Tags:        -
Annotations: 2

#1  2026-09-10T23:40:00Z             deploy-bot  Deploy 812 starts rolling out to checkout
#2  2026-09-10T23:58:12Z  +18m12s    grafana     Checkout p99 latency alert fires
#3  2026-09-11T00:02:30Z  +4m18s     mongod      Replica set has no primary
#4  2026-09-13T14:05:00Z  +62h2m30s  support     Customer reports checkout is still slow
#5  2026-09-14T02:00:00Z  +11h55m0s  deploy-bot  Rolled back deploy 812
```

The column after each time is the time since the event before it started.

## Export the timeline

An export is what you share. Export the timeline as Markdown:

```sh
coroner timeline export --format md
```

coroner prints the Markdown, with times in UTC:

```markdown
# Checkout latency spike (checkout-latency)

- **Refs:** INC-4821, https://tracker.example.com/INC-4821
- **Window:** 2026-09-10 23:40:00 → 2026-09-14 03:00 UTC
- **Events:** 5 · **Sources:** `deploy-bot`, `grafana`, `mongod`, `support`
- **Exported:** 2026-09-14 05:11 UTC by coroner v0.0.0-20260914051033-1fde2b657288

## 2026-09-10 (UTC)

- **23:40:00** · `deploy-bot` · Deploy 812 starts rolling out to checkout
- **23:58:12** · `grafana` · Checkout p99 latency alert fires
  - evidence (julian): `[FIRING:1] CheckoutP99High checkout p99 = 4.1s (threshold 3s)`

## 2026-09-11 (UTC)

- **00:02:30–00:05:40** · `mongod` · Replica set has no primary
  - _comment (julian):_ The election starts 4 minutes after the alert, so it is a symptom, not the trigger

## 2026-09-13 (UTC)

- **14:05** · `support` · Customer reports checkout is still slow

## 2026-09-14 (UTC)

- **02:--** · `deploy-bot` · Rolled back deploy 812
```

Each event shows only as much of its time as you gave: `14:05` for "yesterday 14:05", and
`02:--` for "3 hours ago", which is precise to the hour. The name in parentheses is the
annotation's author, which is your user name.

## What you built

You created a timeline for an incident, added events from exact and remembered times,
attached evidence and a comment, found the gaps in the viewer, and exported the result.

To stop using the tutorial database, run `unset CORONER_DB`, then delete
`coroner-tutorial.db` and `coroner-tutorial.db.lock` from the directory you started in.
For the jobs you do during a real investigation, such as capturing Slack messages and log
lines or sharing the export, see the [how-to guides](../how-to/index.md).
