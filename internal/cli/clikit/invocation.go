package clikit

import (
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
)

// Invocation is how one command line runs: where and how it prints, the configuration, and the
// terminal it may take over, read once from the global flags, App and the environment.
type Invocation struct {
	Out Output
	Cfg config.Config
	// TZ is --tz as given; zone() resolves it.
	TZ                 *string
	Now                func() time.Time
	OutColor, ErrColor bool
	InputOff           bool
	// Terminal is interactiveTerminal's answer: a form may take over the terminal. A viewer or
	// picker asks interactive() instead, which --format and --json also turn off.
	Terminal bool
	// formatGiven is whether --format changed: plain output even on a terminal (I-1).
	formatGiven bool
	// EchoArgs is --db and --config as given, which a form's echoed command repeats.
	EchoArgs []string
	// Timelines is the timeline input under the command's rule; wrapRunErrors has already refused
	// a bad one.
	Timelines TimelineInput
}

// ReadInvocation reads cmd's command line, app and the environment once. It opens no store and
// resolves no zone. A command reads its own flags first, then the invocation, unless a check needs
// it. Never call it from agent skill, version, shell completion or ErrorStyleFor, so a malformed
// config file cannot break them. It logs, at debug, where the database path, config file and
// default time zone came from, and the zone --tz names, so a command calls it once.
func ReadInvocation(cmd *cobra.Command, app App) (Invocation, error) {
	o, err := newOutput(cmd, app)
	if err != nil {
		return Invocation{}, err
	}
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return Invocation{}, err
	}
	flags := cmd.Flags()
	tz := ChangedString(flags, "tz")
	logConfig(app.Logger(), cfg, tz)

	inputOff := flagOrEnv(flags, "no-input", envTrue(app.LookupEnv, "CORONER_NO_INPUT"))
	noColor, _ := readSwitches(flags, app.LookupEnv)
	timelines, err := ReadTimelineInput(flags, flags.Args(), RuleOf(cmd))
	if err != nil {
		return Invocation{}, err
	}

	return Invocation{
		Out: o, Cfg: cfg, TZ: tz, Now: app.Now,
		OutColor:    colorEnabled(app.OutIsTTY, noColor, app.LookupEnv),
		ErrColor:    colorEnabled(app.ErrIsTTY, noColor, app.LookupEnv),
		InputOff:    inputOff,
		Terminal:    interactiveTerminal(app, inputOff),
		formatGiven: flags.Changed("format"),
		EchoArgs:    givenStoreArgs(flags),
		Timelines:   timelines,
	}, nil
}

// ReadInvocationInZone reads the invocation as ReadInvocation does, and the zone its times read
// and print in.
func ReadInvocationInZone(cmd *cobra.Command, app App) (Invocation, Zone, error) {
	inv, err := ReadInvocation(cmd, app)
	if err != nil {
		return Invocation{}, Zone{}, err
	}
	z, err := inv.Zone()

	return inv, z, err
}

// logConfig logs where the database path, config file and default time zone came from, and the
// zone --tz names when tz, the flag's value, is not nil.
func logConfig(logger *slog.Logger, cfg config.Config, tz *string) {
	logger.Debug("database path resolved", "path", cfg.DBPath, "by", cfg.Layers.DBPath)
	logger.Debug("config file resolved", "path", cfg.ConfigPath, "by", cfg.Layers.ConfigPath)
	logger.Debug("default time zone resolved", "zone", cfg.DefaultTimezone, "by", cfg.Layers.DefaultTimezone)
	if tz != nil {
		logger.Debug("time zone resolved", "zone", *tz, "by", "flag")
	}
}

// givenStoreArgs returns the --db and --config args flags were given, in that order.
func givenStoreArgs(flags *pflag.FlagSet) []string {
	var args []string
	for _, name := range []string{"db", "config"} {
		if value := ChangedString(flags, name); value != nil {
			args = append(args, "--"+name, *value)
		}
	}

	return args
}

// Zone returns the zone times read and print in: --tz, else default_timezone, else UTC.
func (inv Invocation) Zone() (Zone, error) {
	return TimeZone(inv.TZ, inv.Cfg.DefaultTimezone, inv.Now().Location())
}

// Interactive reports whether a viewer or picker may open (I-1, I-2).
func (inv Invocation) Interactive() bool {
	return inv.Terminal && !inv.formatGiven && inv.Out.Format != FormatJSON
}

// interactiveTerminal reports whether a command may take over app's terminal: input is not turned
// off, keys can arrive, and the screen is a terminal that can draw (I-1, I-2).
func interactiveTerminal(app App, inputOff bool) bool {
	term, _ := app.LookupEnv("TERM")
	return !inputOff && app.InIsTTY && app.OutIsTTY && term != "dumb"
}
