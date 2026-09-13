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
mise run setup      # install pinned tools and Go modules, register git hooks
mise run preflight  # lint, test, build, and verify the lint config: run before pushing
```

## Status

Pre-0.1 and unstable: commands, flags, and the database schema can change without notice.

## License

coroner is licensed under the `MIT` license; see [LICENSE](LICENSE).
