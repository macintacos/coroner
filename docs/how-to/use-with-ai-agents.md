# Use coroner with AI agents

*Audience: people who want a coding agent, such as Claude Code, to record coroner
timelines for them.*

coroner ships an [Agent Skill](https://agentskills.io/specification) that teaches a coding
agent to build a timeline the way a script should: flags and JSON only, times in RFC 3339,
the timeline named on every command, and writes that are safe to retry. An agent that
supports Agent Skills loads it when you ask it to investigate an outage, reconstruct what
happened when or write a postmortem timeline, or when you mention coroner.

The skill is the
[`skills/coroner`](https://github.com/macintacos/coroner/tree/main/skills/coroner)
directory: `SKILL.md`, and two references the agent reads only when it needs them, one for
adding many events from a JSONL file and one for reading times from MongoDB, syslog and
Slack.

## Install it as a Claude Code plugin

The coroner repository is also a Claude Code plugin marketplace. In Claude Code, add it
and install the `coroner` plugin, which holds only the skill:

```text
/plugin marketplace add macintacos/coroner
/plugin install coroner@coroner
```

From a terminal, the same two steps are:

```sh
claude plugin marketplace add macintacos/coroner
claude plugin install coroner@coroner
```

The marketplace follows the repository's `main` branch. To get a later skill, run
`claude plugin marketplace update coroner`. To remove it:

```sh
claude plugin uninstall coroner@coroner
claude plugin marketplace remove coroner
```

## Install it by copying the directory

Any agent that reads Agent Skills from a directory can use a copy of `skills/coroner`,
references included. For Claude Code, that directory is `~/.claude/skills/` for every
project, or `.claude/skills/` inside one project:

```sh
git clone --depth 1 https://github.com/macintacos/coroner.git /tmp/coroner-src
mkdir -p ~/.claude/skills
cp -R /tmp/coroner-src/skills/coroner ~/.claude/skills/
```

To take the skill from a release rather than `main`, add `--branch v0.1.0`, naming your
coroner's version, to `git clone`. To update the copy, remove it and copy it again. To
remove it:

```sh
rm -r ~/.claude/skills/coroner
```

## Check the skill matches your coroner

Every coroner binary carries the `SKILL.md` it was built with, and prints it:

```sh
coroner agent skill
```

Compare it with an installed copy; `diff` prints nothing when they match:

```sh
coroner agent skill | diff - ~/.claude/skills/coroner/SKILL.md
```

`coroner agent skill` prints `SKILL.md` alone. The references come only with the
directory, so install the skill with one of the methods above rather than from this
output.

## Give an agent the whole CLI

The skill teaches rules and common steps, not every flag. For everything else, point the
agent at one command, which prints every command, flag and help topic as a single Markdown
page and never opens the database:

```sh
coroner help reference
```

## What the skill tells an agent

- Pass `--json`, parse stdout, and decide what happened from the exit code, never from
  message text.
- Name the timeline with `-t` on every command, rather than relying on
  `coroner timeline use`.
- Send RFC 3339 times with an explicit offset, never natural language, which is
  experimental.
- Record what happened as events with a source, keep log lines and messages word for word
  as evidence annotations, and put its reading of them in comment annotations. Hypotheses
  and fixes stay out of the timeline.
- Add more than a handful of events from a JSONL file, with a dry run first.
- Give each event its own id, so a retried command adds nothing twice, and never delete to
  retry.

[The stability policy](https://github.com/macintacos/coroner/blob/main/STABILITY.md) says
which commands, flags and JSON shapes the skill can rely on.
