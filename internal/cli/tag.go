package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func newTagCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tag",
		Short:   "List the tags events carry",
		GroupID: coreCommands,
		Long: "A tag is a free lowercase keyword on an event, such as phase:detect. Tags exist only on\n" +
			"the events that carry them, across the whole database.",
		Example: "  $ coroner tag list",
		// Help and the unknown-verb error print under any format.
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		RunE:        runNoun,
	}

	cmd.AddCommand(newTagListCommand(ctx, app))

	return cmd
}
