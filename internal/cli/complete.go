package cli

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/timeline"
)

// addTimelineCompletion completes timeline names for -t on every command under cmd that
// declares it.
func addTimelineCompletion(ctx context.Context, cmd *cobra.Command, app App) {
	if cmd.Flags().Lookup("timeline") != nil {
		// Registering fails only for a missing or already registered flag.
		_ = cmd.RegisterFlagCompletionFunc("timeline",
			func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
				return completeTimelineNames(ctx, cmd, app, toComplete)
			})
	}

	for _, child := range cmd.Commands() {
		addTimelineCompletion(ctx, child, app)
	}
}

// completeTimelineArg is the ValidArgsFunction of a command whose one positional is a
// timeline name or id.
func completeTimelineArg(ctx context.Context, app App) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		return completeTimelineNames(ctx, cmd, app, toComplete)
	}
}

// completeTimelineNames returns the names starting with toComplete of the timelines in the
// database the command line selects, each described by its title. A failure offers none
// and logs to BASH_COMP_DEBUG_FILE, because stdout is the shell's completion protocol.
func completeTimelineNames(
	ctx context.Context, cmd *cobra.Command, app App, toComplete string,
) ([]cobra.Completion, cobra.ShellCompDirective) {
	timelines, err := readTimelines(ctx, cmd, app)
	if err != nil {
		cobra.CompDebugln(err.Error(), false)
		return nil, cobra.ShellCompDirectiveError | cobra.ShellCompDirectiveNoFileComp
	}

	var completions []cobra.Completion
	for _, t := range timelines {
		if strings.HasPrefix(t.Name, toComplete) {
			completions = append(completions, cobra.CompletionWithDesc(t.Name, t.Title))
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}

// readTimelines returns every timeline in the database the command line selects, and none
// when that database does not exist or is below the latest schema. It never creates,
// migrates or writes the database.
func readTimelines(ctx context.Context, cmd *cobra.Command, app App) ([]timeline.Timeline, error) {
	resolved, err := resolveConfig(cmd, app)
	if err != nil {
		return nil, err
	}

	s, err := app.OpenStoreReadOnly(ctx, resolved.DBPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}

	schema, err := s.Schema(ctx)
	var timelines []timeline.Timeline
	if err == nil && schema.Version == schema.Latest {
		// ponytail: Timelines also reads every timeline's refs; add a name and title query if
		// completion misses R-2's 100 ms.
		timelines, err = s.Timelines(ctx)
	}

	return timelines, errors.Join(err, s.Close())
}
