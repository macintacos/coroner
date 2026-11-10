package clikit

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The ids of the groups root help lists commands under.
const (
	CoreCommands       = "core"
	AdditionalCommands = "additional"
)

// RunNoun is the RunE of a command that only groups verbs. It prints help without a verb
// and rejects an unknown one, which cobra checks only on root.
func RunNoun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	message := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	// cobra defaults the distance to 2 only when it suggests for root.
	cmd.SuggestionsMinimumDistance = cmp.Or(cmd.SuggestionsMinimumDistance, 2)
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		message += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t")
	}

	return UsageError(cmd, errors.New(message))
}

// GlobalFlags declares the flags every command accepts. Each call returns a fresh set.
func GlobalFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("global", pflag.ContinueOnError)
	flags.String("db", "", "Database file `path` (env: CORONER_DB)")
	flags.String("config", "", "Config file `path` (env: CORONER_CONFIG)")
	flags.String("format", string(FormatText), "Output `format`: {text | json | jsonl}, and more on timeline export")
	flags.Bool("json", false, "Short for --format json")
	flags.BoolP("quiet", "q", false, "Print only essential output")
	flags.Bool("no-color", false, "Disable color (env: NO_COLOR, CORONER_NO_COLOR)")
	flags.Bool("no-input", false, "Never open a form, picker or viewer (env: CORONER_NO_INPUT)")
	flags.String("tz", "", tzUsage)
	flags.Bool("debug", false, "Print debug detail on stderr (env: CORONER_DEBUG)")
	flags.BoolP("help", "h", false, "Show help for a command")

	return flags
}
