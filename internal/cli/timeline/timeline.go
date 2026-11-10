// Package timeline holds the `coroner timeline` commands. Each noun's package is named for
// its noun, so importers alias this one timelinecmd: internal/timeline holds the domain types.
package timeline

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/timeline"
)

// NewCommand returns the timeline command and its verbs, which run against app.
func NewCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "timeline",
		Short:   "Create, list and switch between timelines",
		GroupID: clikit.CoreCommands,
		Long: "A timeline is the named chronology of one investigation. Create one per incident or\n" +
			"ticket, then use it so later commands know where events go.",
		Example: "  $ coroner timeline create mongo-outage-2026-09-11 --ref INC-4821\n" +
			"  $ coroner timeline use mongo-outage-2026-09-11\n" +
			"  $ coroner timeline list",
		RunE: clikit.RunNoun,
	}

	cmd.AddCommand(
		newTimelineCreateCommand(ctx, app),
		newTimelineListCommand(ctx, app),
		newTimelineViewCommand(ctx, app),
		newTimelineExportCommand(ctx, app),
		newTimelineUseCommand(ctx, app),
		newTimelineEditCommand(ctx, app),
		newTimelineDeleteCommand(ctx, app),
	)

	return cmd
}

// checkTimeline returns the first rule t breaks as an *Error: an invalid name as invalid_name
// with a suggested name, anything else as usage.
func checkTimeline(t timeline.Timeline) error {
	err := CheckName(t.Name)
	if err != nil {
		return err
	}

	return clikit.RecordError(t.Validate(), "A ref is a ticket key or URL.")
}

// CheckName returns invalid_name, with a suggested name, unless name is a valid timeline name.
func CheckName(name string) error {
	err := timeline.ValidateTimelineName(name)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_' and '-'; put free text in --title."
	if suggestion := SuggestName(name); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`? Put free text in --title.", suggestion)
	}

	return &clikit.Error{Kind: clikit.KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// SuggestName returns the valid timeline name CheckName suggests for name, or "" when name is valid
// or nothing valid remains of it.
func SuggestName(name string) string {
	if timeline.ValidateTimelineName(name) == nil {
		return ""
	}

	return timeline.SuggestTimelineName(name)
}

// withOneArg returns run behind cobra.ExactArgs(1), failing as Run reports cobra's own rejection,
// for a command whose form opens without its argument.
func withOneArg(run clikit.InvokedRunE) clikit.InvokedRunE {
	return func(cmd *cobra.Command, inv clikit.Invocation, args []string) error {
		err := cobra.ExactArgs(1)(cmd, args)
		if err != nil {
			return clikit.ArgsError(cmd, err)
		}

		return run(cmd, inv, args)
	}
}

// oneArgOrNone accepts no argument, which a form asks for, and rejects more than one as
// cobra.ExactArgs(1) does.
func oneArgOrNone(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return cobra.ExactArgs(1)(cmd, args)
	}

	return nil
}

func timelineExistsError(name, hint string, err error) *clikit.Error {
	return &clikit.Error{Kind: clikit.KindTimelineExists, Message: fmt.Sprintf("timeline %q already exists", name), Hint: hint, Err: err}
}

// printTimeline prints t when o asks for JSON; text output leaves it to the command's message.
func printTimeline(o clikit.Output, t timeline.Timeline) error {
	if o.Format != clikit.FormatJSON {
		return nil
	}

	return o.JSON(clikit.NewTimelineJSON(t))
}
