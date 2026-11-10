// Package tag holds the `coroner tag` commands.
package tag

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
)

// NewCommand returns the tag command and its verbs, which run against app.
func NewCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tag",
		Short:   "List the tags events carry",
		GroupID: clikit.CoreCommands,
		Long: "A tag is a free lowercase keyword on an event, such as phase:detect. Tags exist only on\n" +
			"the events that carry them, across the whole database.",
		Example: "  $ coroner tag list",
		RunE:    clikit.RunNoun,
	}

	cmd.AddCommand(newTagListCommand(ctx, app))

	return cmd
}
