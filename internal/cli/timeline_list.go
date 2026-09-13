package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineListCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List timelines, marking the current one",
		Long: "List every timeline, oldest first. A '*' marks the current timeline, the one\n" +
			"'coroner timeline use' selected. Each row is the marker and name, a tab, then the title.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example:     "  $ coroner timeline list\n  $ coroner timeline list --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(ctx, cmd, app, func(s *store.Store) error {
				listed, err := listTimelines(ctx, s)
				if err != nil {
					return err
				}

				return printTimelines(cmd, app, listed)
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

// listTimelines returns every timeline, oldest first, each marked whether it is current.
func listTimelines(ctx context.Context, s *store.Store) ([]listedTimelineJSON, error) {
	timelines, err := s.Timelines(ctx)
	if err != nil {
		return nil, err
	}
	current, err := s.CurrentTimeline(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	slices.SortFunc(timelines, func(a, b timeline.Timeline) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})

	listed := make([]listedTimelineJSON, 0, len(timelines))
	for _, t := range timelines {
		listed = append(listed, listedTimelineJSON{newTimelineJSON(t), t.ID == current.ID})
	}

	return listed, nil
}

func printTimelines(cmd *cobra.Command, app App, listed []listedTimelineJSON) error {
	if printsJSON(cmd) {
		return writeJSON(app.Out, app.OutIsTTY, timelineListJSON{Timelines: listed})
	}

	if len(listed) == 0 {
		return notify(cmd, app, "No timelines yet. Create one with: coroner timeline create <name>\n")
	}

	for _, t := range listed {
		marker := " "
		if t.Current {
			marker = "*"
		}

		_, err := fmt.Fprintf(app.Out, "%s %s\t%s\n", marker, t.Name, t.Title)
		if err != nil {
			return err
		}
	}

	return nil
}
