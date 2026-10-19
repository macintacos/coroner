package cli

import (
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
)

// invocation is how one command line runs: where and how it prints, the configuration, and the
// terminal it may take over, read once from the global flags, App and the environment.
type invocation struct {
	out output
	cfg config.Config
	// tz is --tz as given; zone() resolves it.
	tz                 *string
	now                func() time.Time
	outColor, errColor bool
	inputOff           bool
	// terminal is interactiveTerminal's answer: a form may take over the terminal. A viewer or
	// picker asks interactive() instead, which --format also turns off.
	terminal bool
	// formatGiven is whether --format changed: plain output even on a terminal (I-1).
	formatGiven bool
	debug       bool
	// echoArgs is --db and --config as given, which a form's echoed command repeats.
	echoArgs []string
}

// readInvocation reads cmd's command line, app and the environment once. It opens no store and
// resolves no zone. A command reads its own flags first, then the invocation, unless a check needs
// it. Never call it from agent skill, version, shell completion or errorStyleFor, so a malformed
// config file cannot break them.
func readInvocation(cmd *cobra.Command, app App) (invocation, error) {
	o, err := newOutput(cmd, app)
	if err != nil {
		return invocation{}, err
	}
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return invocation{}, err
	}

	flags := cmd.Flags()
	inputOff := envTrue(app.LookupEnv, "CORONER_NO_INPUT")
	if flags.Changed("no-input") {
		inputOff, _ = flags.GetBool("no-input")
	}
	noColor, _ := flags.GetBool("no-color")
	debug, _ := flags.GetBool("debug")

	return invocation{
		out: o, cfg: cfg, tz: changedString(flags, "tz"), now: app.Now,
		outColor:    colorEnabled(app.OutIsTTY, noColor, app.LookupEnv),
		errColor:    colorEnabled(app.ErrIsTTY, noColor, app.LookupEnv),
		inputOff:    inputOff,
		terminal:    interactiveTerminal(app, inputOff),
		formatGiven: flags.Changed("format"),
		debug:       debug || envTrue(app.LookupEnv, "CORONER_DEBUG"),
		echoArgs:    givenStoreArgs(flags),
	}, nil
}

// givenStoreArgs returns the --db and --config args flags were given, in that order.
func givenStoreArgs(flags *pflag.FlagSet) []string {
	var args []string
	for _, name := range []string{"db", "config"} {
		if value := changedString(flags, name); value != nil {
			args = append(args, "--"+name, *value)
		}
	}

	return args
}

// zone returns the zone times read and print in: --tz, else default_timezone, else local.
func (inv invocation) zone() (zone, error) {
	return timeZone(inv.tz, inv.cfg.DefaultTimezone, inv.now().Location())
}

// interactive reports whether a viewer or picker may open (I-1, I-2).
func (inv invocation) interactive() bool {
	return inv.terminal && !inv.formatGiven
}

// interactiveTerminal reports whether a command may take over app's terminal: input is not turned
// off, keys can arrive, and the screen is a terminal that can draw (I-1, I-2).
func interactiveTerminal(app App, inputOff bool) bool {
	term, _ := app.LookupEnv("TERM")
	return !inputOff && app.InIsTTY && app.OutIsTTY && term != "dumb"
}
