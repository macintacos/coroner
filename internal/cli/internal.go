package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func newInternalCommand(_ context.Context, _ App) *cobra.Command {
	return &cobra.Command{
		Use:   "__internal",
		Short: "Debug coroner itself",
		Long: "Commands that debug coroner itself, not an incident. Help and completion leave them out,\n" +
			"and any of them may change or disappear in any release.",
		Hidden:      true,
		Annotations: map[string]string{diagnosticLogAnnotation: "off"},
		RunE:        runNoun,
	}
}
