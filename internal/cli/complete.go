package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/timeline"
)

// addTimelineCompletion completes timeline names for -t on every command under cmd that
// declares it.
func addTimelineCompletion(ctx context.Context, cmd *cobra.Command, app App) {
	if cmd.Flags().Lookup("timeline") != nil || cmd.PersistentFlags().Lookup("timeline") != nil {
		// The lookup rules out a missing flag, and the walk visits each command once.
		_ = cmd.RegisterFlagCompletionFunc("timeline", completeTimelineNames(ctx, app))
	}

	for _, child := range cmd.Commands() {
		addTimelineCompletion(ctx, child, app)
	}
}

// completeTimelineArg is the ValidArgsFunction of a command whose one positional is a
// timeline name or id.
func completeTimelineArg(ctx context.Context, app App) cobra.CompletionFunc {
	names := completeTimelineNames(ctx, app)
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 || cmd.Flags().Changed("timeline") {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		return names(cmd, args, toComplete)
	}
}

// completeTimelineNames returns the names of the timelines in the database the command line
// selects that start with toComplete, each described by its title. A failure offers none
// and reports the error on app.Err and in BASH_COMP_DEBUG_FILE, because stdout is the
// shell's completion protocol.
func completeTimelineNames(ctx context.Context, app App) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		timelines, err := readTimelines(ctx, cmd, app)
		if err != nil {
			// A completion cannot fail the command; the debug file still gets the error.
			_, _ = fmt.Fprintln(app.Err, "Error: completing timeline names:", err)
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
	// Below the latest schema only Schema may be read, and the next write migrates it, so it
	// is no failure.
	if err == nil && schema.Version >= schema.Latest {
		// ponytail: Timelines also reads every timeline's refs; add a name and title query if
		// completion misses R-2's 100 ms.
		timelines, err = s.Timelines(ctx)
	}

	return timelines, errors.Join(err, s.Close())
}
