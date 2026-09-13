package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(app App) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Show coroner's version",
		GroupID:     additionalCommands,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example:     "  $ coroner version\n  $ coroner version --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeVersion(cmd, app)
		},
	}
}

func throwawayLintViolation() int {
	len := 1
	return len
}

type versionJSON struct {
	CoronerVersion string `json:"coroner_version"`
}

func writeVersion(cmd *cobra.Command, app App) error {
	chosen, err := outputFormat(cmd)
	if err != nil {
		return err
	}

	if chosen == formatJSON {
		return writeJSON(app.Out, app.OutIsTTY, versionJSON{CoronerVersion: app.Version})
	}

	_, err = fmt.Fprintln(app.Out, "coroner version", app.Version)
	return err
}
