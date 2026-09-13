// Package cmd holds coroner's cobra commands.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRoot builds the coroner root command.
func NewRoot() *cobra.Command {
	return &cobra.Command{
		Use:   "coroner",
		Short: "coroner CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Hello, world!")
			return err
		},
	}
}
