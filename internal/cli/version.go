package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/export"
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

type versionJSON struct {
	CoronerVersion string   `json:"coroner_version"`
	FormatVersions []string `json:"format_versions"`
}

func writeVersion(cmd *cobra.Command, app App) error {
	chosen, err := outputFormat(cmd)
	if err != nil {
		return err
	}

	if chosen == formatJSON {
		return writeJSON(app.Out, app.OutIsTTY, versionJSON{CoronerVersion: app.Version, FormatVersions: export.FormatVersions()})
	}

	_, err = fmt.Fprintln(app.Out, "coroner version", app.Version)
	return err
}
