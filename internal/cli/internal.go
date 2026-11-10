package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
)

func newInternalCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "__internal",
		Short: "Debug coroner itself",
		Long: "Commands that debug coroner itself, not an incident. Root help, the reference topic and\n" +
			"shell completion leave this group out, and any command under it may change or disappear in\n" +
			"any release.",
		Hidden:      true,
		Annotations: map[string]string{clikit.DiagnosticLogAnnotation: "off"},
		RunE:        clikit.RunNoun,
	}
	cmd.AddCommand(newInternalLogCommand(ctx, app))

	return cmd
}
