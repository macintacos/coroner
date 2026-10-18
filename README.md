# coroner

coroner is a command-line tool for building root-cause investigation timelines. Name a
timeline for a ticket, add timestamped events from logs, chat, and people, attach evidence
and comments to those events, then view, query, and export the timeline to share what
happened and when.

## Install

```sh
go install github.com/macintacos/coroner@latest
```

[Install coroner](docs/how-to/install.md) covers Homebrew, release archives, shell
completions, uninstalling, and why `go install` is the macOS method for now.

## Use with AI agents

coroner ships an Agent Skill that teaches coding agents, such as Claude Code, to record
timelines with coroner. In Claude Code:

```text
/plugin marketplace add macintacos/coroner
/plugin install coroner@coroner
```

[Use coroner with AI agents](docs/how-to/use-with-ai-agents.md) covers copying the skill
into any agent's skills directory instead, removing it, and checking it matches your
coroner.

## Development

```sh
mise run setup       # install pinned tools and Go modules, register git hooks
mise run docs:serve  # preview the docs site from docs/
mise run preflight   # lint, test, and build the binary and docs: run before pushing
```

On every pull request and push to `main`, CI runs lint and tests, builds the docs, and
fails on unformatted or stale generated files. A pull request that changes the CLI's code
without changing a hand-written page under `docs/` fails unless it has the
`docs-not-needed` label. Pushing a `v*` tag publishes a release;
[the release checklist](docs/contributing/releasing.md) covers the steps around it.

## Status

Pre-0.1 and unstable: any release before 1.0 may change coroner, but never silently.
[STABILITY.md](STABILITY.md) says what coroner promises to keep working, how a change is
announced, and how a command or flag is deprecated before it goes.

## License

coroner is licensed under the `MIT` license; see [LICENSE](LICENSE).
