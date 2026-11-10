package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
)

func WriteMigratedDatabase(path string) { writeMigratedDatabase(path) }

// SeededHome is seededHome for the external specs.
type SeededHome struct{ home seededHome }

func (h *SeededHome) Into(home string, seed func()) { h.home.into(home, seed) }

func CheckCommandLine(cmd *cobra.Command, args []string) error { return checkCommandLine(cmd, args) }

func WalkCommands(cmd *cobra.Command, visit func(*cobra.Command)) { walkCommands(cmd, visit) }

func WriteVerbs() []string { return writeVerbs() }

func CommandOf(root *cobra.Command, args []string) (string, bool) {
	return clikit.CommandOf(root, args)
}

const LogRotateSize = clikit.LogRotateSize

// RunWithThrowawayDeprecation is Run with one more global flag, --throwaway, deprecated in
// favour of --json.
func RunWithThrowawayDeprecation(ctx context.Context, app clikit.App) int {
	app, root := newRun(ctx, app)
	root.PersistentFlags().Bool("throwaway", false, "A flag only tests declare")
	clikit.DeprecateFlag(root.PersistentFlags(), "throwaway",
		clikit.Deprecation{Replacement: "use --json instead", RemovalVersion: "v0.9.0", RemovalDate: "2027-03-14"})

	return execute(ctx, app, root)
}
