package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTagListCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List tags with how often and when they were used",
		Long: "List every tag in the database by name. Each row is the tag, the events carrying it, and\n" +
			"when the first and last of them happened. Piped, the fields are tab-separated.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example:     "  $ coroner tag list\n  $ coroner tag list --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := newOutput(cmd, app)
			if err != nil {
				return err
			}

			return withStore(ctx, cmd, app, func(s *store.Store) error {
				usages, err := s.TagUsages(ctx)
				if err != nil {
					return err
				}

				return printTags(o, usages)
			})
		},
	}
}

type tagListJSON struct {
	Tags      []tagJSON `json:"tags"`
	Total     int       `json:"total"`
	Truncated bool      `json:"truncated"`
}

type tagJSON struct {
	Name            string `json:"name"`
	EventCount      int    `json:"event_count"`
	FirstOccurredAt string `json:"first_occurred_at"`
	LastOccurredAt  string `json:"last_occurred_at"`
}

func printTags(o output, usages []store.TagUsage) error {
	if o.format == formatJSON {
		return o.json(newTagListJSON(usages))
	}
	if len(usages) == 0 {
		return o.notify("No tags yet. Add one with: coroner event add <text> --tag <tag>")
	}

	return o.rows(tagRows(usages))
}

func newTagListJSON(usages []store.TagUsage) tagListJSON {
	// Never nil, so no tags encodes as [] rather than null.
	tags := make([]tagJSON, 0, len(usages))
	for _, u := range usages {
		tags = append(tags, tagJSON{
			Name:            u.Name,
			EventCount:      u.EventCount,
			FirstOccurredAt: timeline.FormatInstant(u.FirstOccurredAt),
			LastOccurredAt:  timeline.FormatInstant(u.LastOccurredAt),
		})
	}

	return tagListJSON{Tags: tags, Total: len(tags)}
}

// tagRows returns a row per tag: its name, event count, and first and last occurred at.
func tagRows(usages []store.TagUsage) [][]string {
	rows := make([][]string, 0, len(usages))
	for _, u := range usages {
		rows = append(rows, []string{
			u.Name, strconv.Itoa(u.EventCount), displayInstant(u.FirstOccurredAt), displayInstant(u.LastOccurredAt),
		})
	}

	return rows
}
