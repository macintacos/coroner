package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "event",
		Short:   "Add, list, view, edit, delete and link events",
		GroupID: coreCommands,
		Long: "An event is something that happened at a moment or over an interval, as a source reported\n" +
			"it. Events go to, and are listed from, the timeline -t names, else CORONER_TIMELINE, else\n" +
			"the one 'coroner timeline use' picked. view, edit, delete, link and unlink also take an\n" +
			"event's id, from any timeline. list takes -t more than once to overlay several timelines.",
		Example: "  $ coroner event add \"Primary stepped down\" --at 2026-09-11T03:14:07Z --source mongod\n" +
			"  $ coroner event list --tag election",
		RunE: runNoun,
	}

	addTimelineFlag(cmd.PersistentFlags())
	cmd.AddCommand(
		newEventAddCommand(ctx, app),
		newEventListCommand(ctx, app),
		newEventViewCommand(ctx, app),
		newEventEditCommand(ctx, app),
		newEventDeleteCommand(ctx, app),
		newEventLinkCommand(ctx, app),
		newEventUnlinkCommand(ctx, app),
	)

	return cmd
}

// eventJSON is the export event with its output-only attachments.
type eventJSON struct {
	export.Event

	// Never nil, so no attachments encode as [] rather than null.
	Attachments []attachmentJSON `json:"attachments"`
}

// newEventJSON returns e's JSON object, with attachments; seq is null until the store
// allocates one.
func newEventJSON(e timeline.Event, attachments []attachmentJSON) eventJSON {
	return eventJSON{Event: export.NewEvent(e), Attachments: append([]attachmentJSON{}, attachments...)}
}
