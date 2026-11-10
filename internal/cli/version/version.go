// Package version holds the version command and the --version flag's output.
package version

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/export"
)

// NewCommand returns the version command.
func NewCommand(app clikit.App) *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Show coroner's version",
		GroupID:     clikit.AdditionalCommands,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json", clikit.DiagnosticLogAnnotation: "off"},
		Example:     "  $ coroner version\n  $ coroner version --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return Write(cmd, app)
		},
	}
}

type versionJSON struct {
	CoronerVersion string   `json:"coroner_version"`
	FormatVersions []string `json:"format_versions"`
}

// Write prints coroner's version in the format cmd asks for.
func Write(cmd *cobra.Command, app clikit.App) error {
	chosen, err := clikit.OutputFormat(cmd)
	if err != nil {
		return err
	}

	if chosen == clikit.FormatJSON {
		return clikit.WriteJSON(app.Out, app.OutIsTTY, versionJSON{CoronerVersion: app.Version, FormatVersions: export.FormatVersions()})
	}

	_, err = fmt.Fprintln(app.Out, "coroner version", app.Version)
	return err
}
