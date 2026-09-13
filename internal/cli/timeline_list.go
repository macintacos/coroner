package cli

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/viewer"
)

func newTimelineListCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List timelines, marking the current one",
		Long: "List every timeline, oldest first. A '*' marks the current timeline, the one\n" +
			"'coroner timeline use' selected.\n\n" +
			"On a terminal it opens a picker that also shows each timeline's refs, event count and\n" +
			"last event: / filters, enter makes the timeline under the cursor current, and q quits.\n" +
			"--format text, --json or piping the output prints the list instead: a marker, the name\n" +
			"and the title on each row, tab-separated when piped.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example:     "  $ coroner timeline list\n  $ coroner timeline list --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := newOutput(cmd, app)
			switch {
			case err != nil:
				return err
			case o.format != formatJSON && interactive(cmd, app):
				return pickTimeline(ctx, cmd, app, o)
			}

			return withStore(ctx, cmd, app, func(s *store.Store) error {
				timelines, currentID, err := listTimelines(ctx, s)
				if err != nil {
					return err
				}

				return printTimelines(o, timelines, currentID)
			})
		},
	}
}

// pickTimeline reads the timelines, closes the database, shows the picker, and makes the
// timeline picked current through timeline use's run function, warning when CORONER_TIMELINE
// still selects another. With no timelines it prints the plain list's hint instead.
func pickTimeline(ctx context.Context, cmd *cobra.Command, app App, o output) error {
	cfg, z, err := resolveZone(cmd, app)
	if err != nil {
		return err
	}
	var rows []viewer.TimelineRow
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		rows, err = readTimelineRows(ctx, s)
		return err
	})
	switch {
	case err != nil:
		return err
	case len(rows) == 0:
		return printTimelines(o, nil, "")
	}

	noColor, _ := cmd.Flags().GetBool("no-color")
	picked, ok, err := viewer.Pick(ctx,
		viewer.Terminal{In: app.In, Out: app.Out, NoColor: !colorEnabled(app.OutIsTTY, noColor, app.LookupEnv)},
		viewer.PickOptions{Rows: rows, Notice: shadowNotice(app.LookupEnv), Location: z.loc})
	switch {
	case errors.Is(err, viewer.ErrInterrupted):
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: err}
	case err != nil || !ok:
		return err
	}

	used, err := useTimeline(ctx, app, cfg, fieldValues{"timeline": {picked.Name}})
	if err == nil {
		err = used.render(o)
	}
	if warning := shadowWarning(app.LookupEnv, picked); err == nil && warning != "" {
		err = o.notify("%s", warning)
	}

	return err
}

// readTimelineRows returns listTimelines' timelines with their event counts and last events.
func readTimelineRows(ctx context.Context, s *store.Store) ([]viewer.TimelineRow, error) {
	timelines, currentID, err := listTimelines(ctx, s)
	if err != nil {
		return nil, err
	}

	rows := make([]viewer.TimelineRow, 0, len(timelines))
	for _, t := range timelines {
		// ponytail: reads every event to count them; add a count and last event query to the
		// store if a timeline ever holds enough events to slow the picker's start.
		events, err := s.Events(ctx, t.ID)
		if err != nil {
			return nil, err
		}

		row := viewer.TimelineRow{Timeline: t, Events: len(events), Current: t.ID == currentID}
		if len(events) > 0 {
			row.Last = &events[len(events)-1]
		}
		rows = append(rows, row)
	}

	return rows, nil
}

type timelineListJSON struct {
	Timelines []listedTimelineJSON `json:"timelines"`
}

// listedTimelineJSON adds current, which is state of this database rather than of the
// timeline, to a timeline's JSON.
type listedTimelineJSON struct {
	timelineJSON

	Current bool `json:"current"`
}

// listTimelines returns every timeline, oldest first, and the current timeline's id, or ""
// when none is current. It sorts because store.Timelines orders by name.
func listTimelines(ctx context.Context, s *store.Store) ([]timeline.Timeline, string, error) {
	timelines, err := s.Timelines(ctx)
	if err != nil {
		return nil, "", err
	}
	current, err := s.CurrentTimeline(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		current.ID = ""
	case err != nil:
		return nil, "", err
	}

	slices.SortFunc(timelines, func(a, b timeline.Timeline) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})

	return timelines, current.ID, nil
}

func printTimelines(o output, timelines []timeline.Timeline, currentID string) error {
	if o.format == formatJSON {
		return o.json(newTimelineListJSON(timelines, currentID))
	}
	if len(timelines) == 0 {
		return o.notify("No timelines yet. Create one with: coroner timeline create <name>")
	}

	return o.rows(timelineRows(timelines, currentID))
}

func newTimelineListJSON(timelines []timeline.Timeline, currentID string) timelineListJSON {
	listed := make([]listedTimelineJSON, 0, len(timelines))
	for _, t := range timelines {
		listed = append(listed, listedTimelineJSON{timelineJSON: newTimelineJSON(t), Current: t.ID == currentID})
	}

	return timelineListJSON{Timelines: listed}
}

// timelineRows returns a row per timeline: a marker, "*" on the current one, its name and title.
func timelineRows(timelines []timeline.Timeline, currentID string) [][]string {
	rows := make([][]string, 0, len(timelines))
	for _, t := range timelines {
		marker := ""
		if t.ID == currentID {
			marker = "*"
		}
		rows = append(rows, []string{marker, t.Name, t.Title})
	}

	return rows
}
