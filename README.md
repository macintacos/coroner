# coroner

coroner is a command-line tool for building root-cause investigation timelines. Name a
timeline for a ticket, add timestamped events from logs, chat, and people, attach evidence
and comments to those events, then view, query, and export the timeline to share what
happened and when.

## Install

```sh
go install github.com/macintacos/coroner@latest
```

## Development

```sh
mise run setup       # install pinned tools and Go modules, register git hooks
mise run docs:serve  # preview the docs site from docs/
mise run preflight   # lint, test, and build the binary and docs: run before pushing
```

On every pull request and push to `main`, CI runs lint and tests, builds the docs, and
fails on unformatted or stale generated files. A pull request that changes the CLI's code
without changing a hand-written page under `docs/` fails unless it has the
`docs-not-needed` label.

## Status

Pre-0.1 and unstable: commands, flags, and the database schema can change without notice.

## License

coroner is licensed under the `MIT` license; see [LICENSE](LICENSE).
