package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func OutputFormat(cmd *cobra.Command) error {
	_, err := outputFormat(cmd)
	return err
}

// RunWithThrowawayDeprecation is Run with one more global flag, --throwaway, deprecated in
// favour of --json.
func RunWithThrowawayDeprecation(ctx context.Context, app App) int {
	root := NewRoot(ctx, app)
	root.PersistentFlags().Bool("throwaway", false, "A flag only tests declare")
	err := deprecateFlag(root.PersistentFlags(), "throwaway",
		deprecation{replacement: "use --json instead", removalVersion: "v0.9.0", removalDate: "2027-03-14"})
	if err != nil {
		panic(err)
	}

	return execute(ctx, app, root)
}
