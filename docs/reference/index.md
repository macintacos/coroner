# Reference

*Audience: people and scripts that need the exact behaviour of a command, a format or a
setting.*

Reference pages describe what coroner does, precisely and without instructions. Pages
generated from coroner's built-in help topics say exactly what `coroner help <topic>`
prints, so the terminal and this site never disagree. To understand why coroner works the
way it does, see the [explanation pages](../explanation/index.md).

- [Export format](export-format.md): the JSON and JSONL an export writes, how it is
  encoded and how it evolves, for scripts and importers. Its machine-readable twin is the
  [JSON Schema](/coroner/schema/export-v1.schema.json).
- [Exit codes](cli/coroner_help_exit-codes.md): what each exit code and error kind means,
  and the JSON error envelope.
- [Environment variables](cli/coroner_help_environment.md): every variable coroner reads.
- [Time input](cli/coroner_help_time.md): the times you can type, the zone they are read
  in, and how they are checked.
- [Output formats](cli/coroner_help_formatting.md): the formats commands print and what
  scripts can rely on in JSON output.
- [Configuration](configuration.md): the config file, its keys, and how flags, environment
  variables and the file take precedence.
- [CLI](cli/coroner.md): every command and flag, generated from coroner itself. In a
  terminal, `coroner help reference` prints them all as one Markdown page, matching the
  installed version.
