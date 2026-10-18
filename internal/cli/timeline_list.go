package cli

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineListCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List timelines, marking the current one",
		Long: "List every timeline, oldest first. Each row is a marker, the name and the title; a '*'\n" +
			"marks the current timeline, the one 'coroner timeline use' selected. Piped, the fields\n" +
			"are tab-separated.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example:     "  $ coroner timeline list\n  $ coroner timeline list --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := newOutput(cmd, app)
			if err != nil {
				return err
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
