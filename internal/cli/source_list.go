package cli

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
)

func newSourceListCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sources with how often and when they were used",
		Long: "List every source in the database by name. Each row is the name, its events, its\n" +
			"annotations, and when its first and last events happened, or '-' when no event names it.\n" +
			"Piped, the fields are tab-separated.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example:     "  $ coroner source list\n  $ coroner source list --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := newOutput(cmd, app)
			if err != nil {
				return err
			}

			return withStore(ctx, cmd, app, func(s *store.Store) error {
				usages, err := s.SourceUsages(ctx)
				if err != nil {
					return err
				}

				return printSources(o, usages)
			})
		},
	}
}

type sourceListJSON struct {
	Sources   []listedSourceJSON `json:"sources"`
	Total     int                `json:"total"`
	Truncated bool               `json:"truncated"`
}

func printSources(o output, usages []store.SourceUsage) error {
	if o.format == formatJSON {
		return o.json(newSourceListJSON(usages))
	}
	if len(usages) == 0 {
		return o.notify("No sources yet. Name one with: coroner event add <text> --source <name>")
	}

	return o.rows(sourceRows(usages))
}

func newSourceListJSON(usages []store.SourceUsage) sourceListJSON {
	// Never nil, so no sources encodes as [] rather than null.
	listed := make([]listedSourceJSON, 0, len(usages))
	for _, u := range usages {
		listed = append(listed, newListedSourceJSON(u))
	}

	return sourceListJSON{Sources: listed, Total: len(listed)}
}

// sourceRows returns a row per source: its name, event and annotation counts, and first and
// last occurred at.
func sourceRows(usages []store.SourceUsage) [][]string {
	rows := make([][]string, 0, len(usages))
	for _, u := range usages {
		rows = append(rows, []string{
			u.Name,
			strconv.Itoa(u.EventCount),
			strconv.Itoa(u.AnnotationCount),
			displayOptionalInstant(u.FirstOccurredAt),
			displayOptionalInstant(u.LastOccurredAt),
		})
	}

	return rows
}
