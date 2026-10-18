# Configuration

*Audience: anyone setting coroner's defaults for their machine, by hand or from a script.*

coroner reads an optional TOML config file for defaults you would otherwise pass on every
command. Nothing needs one: without a file, every key takes its default.

## Where the file lives

coroner takes the config file from the first of:

1. `--config <path>`.
2. `CORONER_CONFIG`.
3. `$XDG_CONFIG_HOME/coroner/config.toml`, or `~/.config/coroner/config.toml` when
   `XDG_CONFIG_HOME` is unset or not an absolute path. This holds on macOS too.

A file named by `--config` or `CORONER_CONFIG` must exist; the default file may be
missing.

```toml
db               = "/Users/sam/incidents/coroner.db"
default_timezone = "America/New_York"
author           = "sam"
```

## Keys

| Key | Type | Default | Effect |
| --- | --- | --- | --- |
| `db` | string | `$XDG_DATA_HOME/coroner/coroner.db`, or `~/.local/share/coroner/coroner.db` | The database file. It must be an absolute path. |
| `default_timezone` | string | `local`, the zone `TZ` sets | The zone a typed time without an offset is read in, and text output shows times in: an IANA zone such as `America/New_York`, or `local`. |
| `author` | string | the value of `USER` | The author of an event or annotation added without `--author`. |

The file holds only these keys: coroner rejects a file with any other key, or with a
relative `db`, naming the file and the key.

## Precedence

A flag wins over its environment variable, an environment variable over the config file,
and the config file over the default:

| Setting | Flag | Environment variable | Config file |
| --- | --- | --- | --- |
| Database | `--db` | `CORONER_DB` | `db` |
| Time zone | `--tz` | | `default_timezone` |
| Author | `--author` | | `author` |

`TZ` and `USER` do not rank above the config file: they only supply the defaults, the
local zone and the author, used when `default_timezone` and `author` are unset. An empty
`CORONER_*` or `XDG_*` variable counts as unset. An offset or zone typed in a time, such
as `Z` or `+02:00`, wins over all of them; see [time input](cli/coroner_help_time.md). The
Markdown, Mermaid, HTML and SVG exports show times in UTC unless `--tz` names a zone, so a
shared document never depends on the exporter's `default_timezone`.

[Environment variables](cli/coroner_help_environment.md) lists every variable coroner
reads, including those the config file has no key for.

## The blob directory

Attachment files are stored beside the database, in a directory named after it:

- `blobs/` beside a database named `coroner.db`, as the default database is;
- `<db>.blobs` beside a database with any other name, so `/data/incidents.db` keeps its
  blobs in `/data/incidents.db.blobs`.

No two database paths share a blob directory. The name is read from the path as written,
so a symlink or a differently cased path to the same database gets a blob directory of its
own.
