// Package event holds the `coroner event` commands.
package event

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

// NewCommand returns the event command and its verbs, which run against app.
func NewCommand(ctx context.Context, app clikit.App, newTimeline clikit.TimelineAsk) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "event",
		Short:   "Add, list, view, edit, delete and link events",
		GroupID: clikit.CoreCommands,
		Long: "An event is something that happened at a moment or over an interval, as a source reported\n" +
			"it. Events go to, and are listed from, the timeline -t names, else CORONER_TIMELINE, else\n" +
			"the one 'coroner timeline use' picked. view, edit, delete, link and unlink also take an\n" +
			"event's id, from any timeline. list takes -t more than once to overlay several timelines.",
		Example: "  $ coroner event add \"Primary stepped down\" --at 2026-09-11T03:14:07Z --source mongod\n" +
			"  $ coroner event list --tag election",
		RunE: clikit.RunNoun,
	}

	clikit.AddTimelineFlag(cmd.PersistentFlags())
	cmd.AddCommand(
		newEventAddCommand(ctx, app, newTimeline),
		newEventListCommand(ctx, app),
		newEventViewCommand(ctx, app),
		newEventEditCommand(ctx, app),
		newEventDeleteCommand(ctx, app),
		newEventLinkCommand(ctx, app),
		newEventUnlinkCommand(ctx, app),
	)

	return cmd
}

// JSON is the export event with its output-only attachments.
type JSON struct {
	export.Event

	// Never nil, so no attachments encode as [] rather than null.
	Attachments []clikit.AttachmentJSON `json:"attachments"`
}

// NewJSON returns e's JSON object, with attachments; seq is null until the store
// allocates one.
func NewJSON(e timeline.Event, attachments []clikit.AttachmentJSON) JSON {
	return JSON{Event: export.NewEvent(e), Attachments: append([]clikit.AttachmentJSON{}, attachments...)}
}
