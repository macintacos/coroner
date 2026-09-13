package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newSourceCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "source",
		Short:   "List sources and fix their names",
		GroupID: coreCommands,
		Long: "A source is the system, service or person an event or annotation came from. Sources\n" +
			"belong to the whole database, not one timeline, and are created the first time an event\n" +
			"or annotation names one.",
		Example: "  $ coroner source list\n" +
			"  $ coroner source edit mongo --name mongodb",
		// Help and the unknown-verb error print under any format.
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		RunE:        runNoun,
	}

	cmd.AddCommand(newSourceListCommand(ctx, app), newSourceEditCommand(ctx, app))

	return cmd
}

// sourceJSON is a source's own keys; listedSourceJSON adds its usage.
type sourceJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// listedSourceJSON is a source's JSON with its usage, as source list and source edit print it.
type listedSourceJSON struct {
	sourceJSON

	EventCount      int     `json:"event_count"`
	AnnotationCount int     `json:"annotation_count"`
	FirstOccurredAt *string `json:"first_occurred_at"`
	LastOccurredAt  *string `json:"last_occurred_at"`
}

func newListedSourceJSON(u store.SourceUsage) listedSourceJSON {
	return listedSourceJSON{
		ID:              u.ID,
		Name:            u.Name,
		CreatedAt:       timeline.FormatInstant(u.CreatedAt),
		EventCount:      u.EventCount,
		AnnotationCount: u.AnnotationCount,
		FirstOccurredAt: formatOptionalInstant(u.FirstOccurredAt),
		LastOccurredAt:  formatOptionalInstant(u.LastOccurredAt),
	}
}

func formatOptionalInstant(t *time.Time) *string {
	if t == nil {
		return nil
	}

	return new(timeline.FormatInstant(*t))
}

// displayOptionalInstant is displayInstant, or "-" for nil.
func displayOptionalInstant(t *time.Time) string {
	if t == nil {
		return "-"
	}

	return displayInstant(*t)
}
