package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "timeline",
		Short:   "Create, list and switch between timelines",
		GroupID: coreCommands,
		Long: "A timeline is the named chronology of one investigation. Create one per incident or\n" +
			"ticket, then use it so later commands know where events go.",
		Example: "  $ coroner timeline create mongo-outage-2026-09-11 --ref INC-4821\n" +
			"  $ coroner timeline use mongo-outage-2026-09-11\n" +
			"  $ coroner timeline list",
		RunE: runNoun,
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

func newTimelineJSON(t timeline.Timeline) export.Timeline {
	return export.NewTimeline(t)
}

// findTimeline returns the timeline called nameOrID, or else the one with that id. A name
// that is another timeline's id is ambiguous.
func findTimeline(ctx context.Context, s records, nameOrID string) (timeline.Timeline, error) {
	t, err := s.TimelineByName(ctx, nameOrID)
	if errors.Is(err, store.ErrNotFound) {
		t, err = s.Timeline(ctx, nameOrID)
	} else if err == nil {
		err = checkUnambiguous(ctx, s, t, nameOrID)
	}
	if errors.Is(err, store.ErrNotFound) {
		return t, &Error{
			Kind:    KindTimelineNotFound,
			Message: fmt.Sprintf("no timeline is named %q or has that id", nameOrID),
			Hint:    "Run 'coroner timeline list' to see the timelines.",
			Err:     err,
		}
	}

	return t, err
}

// checkUnambiguous returns ambiguous_id when a timeline other than named has nameOrID as its
// id.
func checkUnambiguous(ctx context.Context, s records, named timeline.Timeline, nameOrID string) error {
	byID, err := s.Timeline(ctx, nameOrID)
	switch {
	case errors.Is(err, store.ErrNotFound) || (err == nil && byID.ID == named.ID):
		return nil
	case err != nil:
		return err
	}

	return &Error{
		Kind:    KindAmbiguousID,
		Message: fmt.Sprintf("`%s` is the name of timeline %s and the id of timeline `%s`", nameOrID, named.ID, byID.Name),
		Hint:    fmt.Sprintf("Pass %s for the first, or `%s` for the second.", named.ID, byID.Name),
	}
}

func timelineIDs(timelines []timeline.Timeline) []string {
	ids := make([]string, 0, len(timelines))
	for _, t := range timelines {
		ids = append(ids, t.ID)
	}

	return ids
}

// readEach returns what read returns for the id of each of timelines, in order.
func readEach[T any](timelines []timeline.Timeline, read func(timelineID string) ([]T, error)) ([]T, error) {
	var all []T
	for _, t := range timelines {
		records, err := read(t.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, records...)
	}

	return all, nil
}

// checkTimeline returns the first rule t breaks as an *Error: an invalid name as invalid_name
// with a suggested name, anything else as usage.
func checkTimeline(t timeline.Timeline) error {
	err := checkName(t.Name)
	if err != nil {
		return err
	}

	return recordError(t.Validate(), "A ref is a ticket key or URL.")
}

// checkName returns invalid_name, with a suggested name, unless name is a valid timeline name.
func checkName(name string) error {
	err := timeline.ValidateTimelineName(name)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_' and '-'; put free text in --title."
	if suggestion := suggestName(name); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`? Put free text in --title.", suggestion)
	}

	return &Error{Kind: KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// suggestName returns the valid timeline name checkName suggests for name, or "" when name is valid
// or nothing valid remains of it.
func suggestName(name string) string {
	if timeline.ValidateTimelineName(name) == nil {
		return ""
	}

	return timeline.SuggestTimelineName(name)
}

// withOneArg returns run behind cobra.ExactArgs(1), failing as Run reports cobra's own rejection,
// for a command whose form opens without its argument.
func withOneArg(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := cobra.ExactArgs(1)(cmd, args)
		if err != nil {
			return argsError(cmd, err)
		}

		return run(cmd, args)
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

func timelineExistsError(name, hint string, err error) *Error {
	return &Error{Kind: KindTimelineExists, Message: fmt.Sprintf("timeline %q already exists", name), Hint: hint, Err: err}
}

// appendUnique appends each value in add that values does not hold yet.
func appendUnique(values []string, add ...string) []string {
	for _, value := range add {
		if !slices.Contains(values, value) {
			values = append(values, value)
		}
	}

	return values
}

// stringArray returns the values of the repeatable flag called name as given: pflag's
// GetStringArray round-trips them through CSV, which drops a lone empty value.
func stringArray(flags *pflag.FlagSet, name string) []string {
	values, _ := flags.Lookup(name).Value.(pflag.SliceValue)
	return values.GetSlice()
}

// printTimeline prints t when o asks for JSON; text output leaves it to the command's message.
func printTimeline(o output, t timeline.Timeline) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newTimelineJSON(t))
}
