package timeline

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/viewer"
)

func newTimelineListCommand(ctx context.Context, app clikit.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List timelines, marking the current one",
		Long: "List every timeline, oldest first. A '*' marks the current timeline, the one\n" +
			"'coroner timeline use' selected.\n\n" +
			"On a terminal it opens a picker that also shows each timeline's refs, event count and\n" +
			"last event: / filters, enter makes the timeline under the cursor current, and q quits.\n" +
			"--format text, --json, --no-input, CORONER_NO_INPUT or piping the output prints the list\n" +
			"instead: a marker, the name and the title on each row, tab-separated when piped.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example:     "  $ coroner timeline list\n  $ coroner timeline list --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inv, err := clikit.ReadInvocation(cmd, app)
			switch {
			case err != nil:
				return err
			case inv.Interactive():
				return pickTimeline(ctx, app, inv)
			}

			return clikit.WithStorePath(ctx, app, inv.Cfg.DBPath, func(s *store.Store) error {
				timelines, currentID, err := listTimelines(ctx, s)
				if err != nil {
					return err
				}
				clikit.LogRead(app.Logger(), clikit.CountOf("timelines", len(timelines)))

				return printTimelines(inv.Out, timelines, currentID)
			})
		},
	}
}

// pickTimeline reads the timelines, closes the database, shows the picker, and makes the
// timeline picked current through timeline use's run function, warning when CORONER_TIMELINE
// still selects another. With no timelines it prints the plain list's hint instead.
func pickTimeline(ctx context.Context, app clikit.App, inv clikit.Invocation) error {
	z, err := inv.Zone()
	if err != nil {
		return err
	}
	var rows []viewer.TimelineRow
	err = clikit.WithStorePath(ctx, app, inv.Cfg.DBPath, func(s *store.Store) error {
		var err error
		rows, err = ReadRows(ctx, s)
		return err
	})
	if err != nil {
		return err
	}
	clikit.LogRead(app.Logger(), clikit.CountOf("timelines", len(rows)))
	if len(rows) == 0 {
		return printTimelines(inv.Out, nil, "")
	}

	picked, ok, err := viewer.Pick(ctx,
		viewer.Terminal{In: app.In, Out: app.Out, NoColor: !inv.OutColor, Env: terminalEnv(app.LookupEnv)},
		viewer.PickOptions{Rows: rows, Notice: clikit.ShadowNotice(app.LookupEnv), Location: z.Loc})
	switch {
	case errors.Is(err, viewer.ErrInterrupted):
		return &clikit.Error{Kind: clikit.KindInterrupted, Message: "interrupted", Err: err}
	case err != nil || !ok:
		return err
	}

	app.Logger().Debug("timeline picked", "timeline", picked.Name)
	used, err := Use(ctx, app, inv.Cfg, clikit.FieldValues{"timeline": {picked.ID}})
	if err != nil {
		return err
	}
	used.LogChanges(ctx, app.Logger())
	return used.Render(inv.Out)
}

// ReadRows returns listTimelines' timelines with their event counts and last events.
func ReadRows(ctx context.Context, s *store.Store) ([]viewer.TimelineRow, error) {
	timelines, currentID, err := listTimelines(ctx, s)
	if err != nil {
		return nil, err
	}

	rows := make([]viewer.TimelineRow, 0, len(timelines))
	for _, t := range timelines {
		page, err := s.ListEvents(ctx, store.EventFilter{TimelineIDs: []string{t.ID}, Order: store.OrderDesc, Limit: 1})
		if err != nil {
			return nil, err
		}

		row := viewer.TimelineRow{Timeline: t, EventCount: page.Total, Current: t.ID == currentID}
		if len(page.Events) > 0 {
			row.Last = &page.Events[0]
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
	export.Timeline

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

func printTimelines(o clikit.Output, timelines []timeline.Timeline, currentID string) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(newTimelineListJSON(timelines, currentID))
	}
	if len(timelines) == 0 {
		return o.Notify("No timelines yet. Create one with: coroner timeline create <name>")
	}

	return o.Rows(timelineRows(timelines, currentID))
}

func newTimelineListJSON(timelines []timeline.Timeline, currentID string) timelineListJSON {
	listed := make([]listedTimelineJSON, 0, len(timelines))
	for _, t := range timelines {
		listed = append(listed, listedTimelineJSON{Timeline: clikit.NewTimelineJSON(t), Current: t.ID == currentID})
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
