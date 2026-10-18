package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAttachmentListCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [-e <ref>]",
		Short: "List a timeline's attachments",
		Long: "List the attachments on a timeline's events and annotations, oldest first. -e lists those\n" +
			"on that event itself. Each row is the id, what it is attached to (an event's seq or an\n" +
			"annotation's id), the filename, size and type; piped, the fields are tab-separated.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner attachment list\n" +
			"  $ coroner attachment list -e 12 --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listAttachments(ctx, cmd, app)
		},
	}

	addTimelineFlag(cmd.Flags())
	cmd.Flags().StringP("event", "e", "", "Only the attachments on this event, as a `ref`: seq, timeline#seq or id")

	return cmd
}

func listAttachments(ctx context.Context, cmd *cobra.Command, app App) error {
	flag, err := timelineFlag(cmd.Flags())
	if err != nil {
		return err
	}
	ref, err := eventFlag(cmd.Flags())
	if err != nil {
		return err
	}
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		t, err := selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		attachments, events, err := readAttachments(ctx, s, t, ref, timelineSelection{flag: flag, lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		if o.format == formatJSON {
			return o.json(newAttachmentListJSON(newAttachmentJSONs(attachments, cfg.DBPath)))
		}
		if ref == nil && len(attachments) > 0 {
			events, err = s.Events(ctx, t.ID)
			if err != nil {
				return err
			}
		}

		return printAttachments(o, attachments, orderEvents(events))
	})
}

// readAttachments returns the attachments on t's events and annotations or, when ref is set, on
// the event it names, with that event.
func readAttachments(
	ctx context.Context, s *store.Store, t timeline.Timeline, ref *eventRef, selection timelineSelection,
) ([]timeline.Attachment, []timeline.Event, error) {
	if ref == nil {
		attachments, err := s.TimelineAttachments(ctx, t.ID)
		return attachments, nil, err
	}

	e, err := findEventIn(ctx, s, t, *ref, selection)
	if err != nil {
		return nil, nil, err
	}
	attachments, err := s.ListAttachments(ctx, timeline.AttachmentOwner{EventID: &e.ID})

	return attachments, []timeline.Event{e}, err
}

// printAttachments prints a row per attachment: its id, the seq of its event from order or the
// id of its annotation, and its filename, size and type.
func printAttachments(o output, attachments []timeline.Attachment, order eventOrder) error {
	if len(attachments) == 0 {
		return o.notify("No attachments yet. Attach a file with: coroner event edit <event> --attach <file>")
	}

	rows := make([][]string, 0, len(attachments))
	for _, a := range attachments {
		var attachedTo string
		if a.Owner.EventID != nil {
			attachedTo = "#" + strconv.FormatInt(order[*a.Owner.EventID].seq, 10)
		} else {
			attachedTo = *a.Owner.AnnotationID
		}
		rows = append(rows, []string{a.ID, attachedTo, a.Filename, formatSize(a.Size), a.MIME})
	}

	return o.rows(rows)
}
