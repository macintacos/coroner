package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(app App) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Show coroner's version",
		GroupID: additionalCommands,
		Args:    cobra.NoArgs,
		Example: "  $ coroner version\n  $ coroner version --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeVersion(cmd, app)
		},
	}
}

type versionJSON struct {
	CoronerVersion string `json:"coroner_version"`
}

func writeVersion(cmd *cobra.Command, app App) error {
	format, err := outputFormat(cmd, formatText, formatJSON)
	if err != nil {
		return err
	}

	if format == formatJSON {
		return writeJSON(app.Out, app.OutIsTTY, versionJSON{CoronerVersion: app.Version})
	}

	_, err = fmt.Fprintln(app.Out, "coroner version", app.Version)
	return err
}
