# Environment variables coroner reads

*Audience: anyone configuring coroner, by hand or from a script.*

A flag wins over its environment variable, and an environment variable wins over the
config file. An empty `CORONER_*` variable counts as unset, and so does an `XDG_*`
variable that is empty or not an absolute path.

| Variable | Effect |
| --- | --- |
| `CORONER_DB` | The database file, as `--db` sets it |
| `CORONER_CONFIG` | The config file, as `--config` sets it; the file must exist |
| `CORONER_TIMELINE` | The current timeline when `-t` names none |
| `CORONER_DEBUG` | Print debug detail on stderr, as `--debug` does, unless it is a false value such as `0` or `false` |
| `NO_COLOR`, `CORONER_NO_COLOR` | Any non-empty value disables color, as `--no-color` does |
| `TERM` | `dumb` disables color |
| `TZ` | The local time zone, used when neither `--tz` nor `default_timezone` names one |
| `XDG_CONFIG_HOME` | The directory holding `coroner/config.toml` |
| `XDG_DATA_HOME` | The directory holding `coroner/coroner.db` |
| `HOME` | The base of the default directories when an `XDG_*` variable is unset |

## Where the config file and database live

coroner uses the XDG directories on macOS too, never `~/Library/Application Support`:

- Config file: `~/.config/coroner/config.toml`.
- Database: `~/.local/share/coroner/coroner.db`.

The config file is taken from `--config`, then `CORONER_CONFIG`, then the default. The
database is taken from `--db`, then `CORONER_DB`, then `db` in the config file, then the
default.

There is no `CORONER_FORMAT`: pass `--format` or `--json` to each command that should
print JSON.
