# Design principles

*Audience: people who want to know the principles coroner's commands and code follow, and
contributors deciding whether a change fits.*

[clig.dev](https://clig.dev/) is the governing reference for coroner's command-line
interface, and following it is mandatory: every new or changed command, flag, output or
error message is checked against it, through the rule IDs in the
[command checklist](../contributing/command-checklist.md). The principles below are
coroner's own, and where one touches the interface, clig.dev still decides.

## Exports are the only sharing contract

coroner keeps every timeline in one SQLite file on your machine. That file is not an
interface: its schema is internal and can change in any release, with coroner migrating it
for you. What leaves the machine is an export, and JSON and JSONL are the contract
scripts, agents and importers rely on. The [export format](../reference/export-format.md)
describes it, a JSON Schema checks it, and fixtures from every released version keep it
readable.

Keeping the contract narrow keeps it stable. A consumer that read the database directly
would pin every table and column forever. Human text output, and the layouts of the
Markdown, Mermaid, HTML and SVG exports, are for people and are not contracts either: they
improve freely, and scripts never parse them.

What coroner promises to keep stable, how it classifies a change, and how it retires a
command or flag are set by
[the stability policy](https://github.com/macintacos/coroner/blob/main/STABILITY.md).

## Flags first, with forms at parity

Every command does all its work from flags and arguments, so a script or an agent can run
it with no terminal. On a terminal, `event add`, `annotation add`, `timeline create` and
`timeline edit` open a form when a required value is missing. A form is a way to fill in
the same flags, never a second interface: each command declares its flags and their checks
once, the form is built from that declaration, and a test fails when a flag has no form
field or the two reject a bad value differently. When a form is submitted, coroner prints
the flags-only command it equals, so what you did interactively you can repeat in a
script.

`--no-input` or `CORONER_NO_INPUT` keeps every form, picker and viewer closed, and a
missing value then exits with a usage error instead of waiting for input that will never
come.

## One way to pick the timeline

A command acts on the timeline named by the first of: `-t`, then `CORONER_TIMELINE`, then
the current timeline `coroner timeline use` stored. With none of them, the command stops
with an error that lists your timelines, or says to create one with
`coroner timeline create` when you have none, rather than guessing one.
`coroner event list`, `coroner timeline export`, `coroner timeline view` and
`coroner overlay create` read several timelines at once and take `-t` more than once to
overlay them; every other command refuses a second `-t`.

```sh
coroner event list -t mongo-outage
CORONER_TIMELINE=mongo-outage coroner event list
coroner timeline use mongo-outage
```

The order runs from most to least specific: a flag describes one command, the variable one
shell, and the stored timeline every command run against that database. When you pick a
timeline in the `coroner timeline list` picker while `CORONER_TIMELINE` names another,
coroner warns that the variable still wins, so a forgotten variable does not quietly send
events to the wrong investigation.

## Honest functions: a pure core in a thin shell

coroner's code reaches the outside world only at its edge. `main.go` builds one value
holding the process's arguments, streams, environment, file system, clock and ID
generator, and every command receives what it needs from that value rather than reading
the process itself. Beneath the commands, the domain model and the time parser are pure:
they take their inputs, including the current time and the zone, as parameters and do no
I/O, and lint rejects a clock read or process access outside `main.go`, the tests and the
dev tools.

A function whose signature is its whole contract can be read on its own and tested without
staging a world around it. That is why coroner's end-to-end tests can pin the clock and
the IDs, and why the same parsing and ordering code serves the commands, the forms, the
viewer and the exports.

## No telemetry, network calls or update checks

coroner never phones home. It collects no telemetry, makes no network calls, and never
checks for updates; an investigation's evidence stays on your machine until you export it.
The HTML export follows the same rule: the page it writes loads nothing over the network.
To upgrade, use the tool you installed coroner with, as
[Install coroner](../how-to/install.md) describes.
