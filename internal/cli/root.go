package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
)

func newRoot(app App) *cobra.Command {
	root := &cobra.Command{
		Use:   "coroner",
		Short: "coroner CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// TODO: hand the resolved Config to the first command that needs it.
			_, err := resolveConfig(cmd, app)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(app.Out, "Hello, world!")
			return err
		},
	}

	root.PersistentFlags().AddFlagSet(globalFlags())
	// Run prints errors once, and usage only on request.
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError(cmd, err)
	})
	wrapRunErrors(root)

	return root
}

// globalFlags declares the flags every command accepts. Each call returns a fresh set.
func globalFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("global", pflag.ContinueOnError)
	flags.String("db", "", "path to the database file")
	flags.String("config", "", "path to the config file")
	flags.String("format", "text", "output format: text, json or jsonl")
	flags.Bool("json", false, "short for --format json")
	flags.BoolP("quiet", "q", false, "print only essential output")
	flags.Bool("no-color", false, "disable color")
	flags.String("tz", "", "time zone for times in this invocation")
	flags.Bool("debug", false, "print debug detail on stderr")

	return flags
}

// wrapRunErrors makes every error a command body returns an *Error, so Run can tell an
// untyped error from cobra, which rejected the command line, from a command's own.
func wrapRunErrors(cmd *cobra.Command) {
	if run := cmd.RunE; run != nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			return asInternal(run(cmd, args))
		}
	}

	for _, child := range cmd.Commands() {
		wrapRunErrors(child)
	}
}

func resolveConfig(cmd *cobra.Command, app App) (config.Config, error) {
	return config.Resolve(config.Flags{
		DBPath:     flagValue(cmd, "db"),
		ConfigPath: flagValue(cmd, "config"),
	}, app.LookupEnv, app.ReadFile)
}

// flagValue returns the named flag's value, or nil when it was not given on the command line.
func flagValue(cmd *cobra.Command, name string) *string {
	flag := cmd.Flags().Lookup(name)
	if !flag.Changed {
		return nil
	}

	return new(flag.Value.String())
}
