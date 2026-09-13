package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRoot(app App) *cobra.Command {
	return &cobra.Command{
		Use:   "coroner",
		Short: "coroner CLI",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(app.Out, "Hello, world!")
			return err
		},
	}
}
