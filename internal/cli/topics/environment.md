# Environment variables coroner reads

*Audience: anyone configuring coroner, by hand or from a script.*

A flag wins over its environment variable, and an environment variable wins over the
config file. An empty `CORONER_*` variable counts as unset, and so does an `XDG_*`
variable that is empty or not an absolute path. A false value is `0`, `f`, `false`, `no`
or `off`, in any case.

Images show inline only with color on and outside a multiplexer.

| Variable | Effect |
| --- | --- |
| `CORONER_DB` | The database file, as `--db` sets it |
| `CORONER_CONFIG` | The config file, as `--config` sets it; the file must exist |
| `CORONER_TIMELINE` | The timeline to act on when `-t` names none; it wins over `coroner timeline use` |
| `CORONER_DEBUG` | Print debug detail on stderr, as `--debug` does, unless it is a false value |
| `CORONER_NO_INPUT` | Never open a form, picker or viewer, as `--no-input` does, unless it is a false value |
| `NO_COLOR`, `CORONER_NO_COLOR` | Any non-empty value disables color, as `--no-color` does |
| `TERM` | `dumb` disables color, the forms, the picker and the viewer; `xterm-kitty` or `xterm-ghostty` shows images inline, and a `tmux` or `screen` value shows placeholders instead |
| `KITTY_WINDOW_ID` | Set by kitty: images show inline |
| `TERM_PROGRAM`, `TERM_PROGRAM_VERSION` | `ghostty`, or `iTerm.app` at version 3.7 or later, shows images inline; another program, such as `vscode`, shows placeholders whatever the other variables say |
| `LC_TERMINAL`, `LC_TERMINAL_VERSION` | `iTerm2` at version 3.7 or later, as iTerm2 passes it through ssh, shows images inline |
| `TMUX`, `STY`, `ZELLIJ` | Set inside tmux, screen or Zellij: images show as placeholders, since the multiplexer drops them |
| `TZ` | The local time zone, used when neither `--tz` nor `default_timezone` names one |
| `XDG_CONFIG_HOME` | The directory holding `coroner/config.toml` |
| `XDG_DATA_HOME` | The directory holding `coroner/coroner.db` |
| `HOME` | The base of the default directories when an `XDG_*` variable is unset, and what a form expands a leading `~/` in Files to |
| `USER` | The author of an event when neither `--author` nor `author` in the config file names one |

## Where the config file and database live

coroner uses the XDG directories on macOS too, never `~/Library/Application Support`:

- Config file: `~/.config/coroner/config.toml`.
- Database: `~/.local/share/coroner/coroner.db`.

The config file is taken from `--config`, then `CORONER_CONFIG`, then the default. The
database is taken from `--db`, then `CORONER_DB`, then `db` in the config file, then the
default.

There is deliberately no `CORONER_FORMAT`: one exported in a shell would silently change
what every script run from it receives. Pass `--format` or `--json` to each command that
should print JSON.
