package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func newInternalCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "__internal",
		Short: "Debug coroner itself",
		Long: "Commands that debug coroner itself, not an incident. Help and completion leave them out,\n" +
			"and any of them may change or disappear in any release.",
		Hidden:      true,
		Annotations: map[string]string{diagnosticLogAnnotation: "off"},
		RunE:        runNoun,
	}
	cmd.AddCommand(newInternalLogCommand(ctx, app))

	return cmd
}
