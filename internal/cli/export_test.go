package cli

import "github.com/spf13/cobra"

func OutputFormat(cmd *cobra.Command) error {
	_, err := outputFormat(cmd)
	return err
}
