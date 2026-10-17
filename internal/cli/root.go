package cli

import (
	"fmt"

	"github.com/spf13/cobra"

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

	root.PersistentFlags().String("db", "", "path to the database file")
	root.PersistentFlags().String("config", "", "path to the config file")

	return root
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
