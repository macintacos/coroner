package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/export"
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
		RunE: runNoun,
	}

	cmd.AddCommand(newSourceListCommand(ctx, app), newSourceEditCommand(ctx, app))

	return cmd
}

// listedSourceJSON is a source's JSON with its usage, as source list and source edit print it.
type listedSourceJSON struct {
	export.Source

	EventCount      int             `json:"event_count"`
	AnnotationCount int             `json:"annotation_count"`
	FirstOccurredAt *export.Instant `json:"first_occurred_at"`
	LastOccurredAt  *export.Instant `json:"last_occurred_at"`
}

func newListedSourceJSON(u store.SourceUsage) listedSourceJSON {
	return listedSourceJSON{
		Source:          export.NewSource(u.Source),
		EventCount:      u.EventCount,
		AnnotationCount: u.AnnotationCount,
		FirstOccurredAt: (*export.Instant)(u.FirstOccurredAt),
		LastOccurredAt:  (*export.Instant)(u.LastOccurredAt),
	}
}

// displayOptionalInstant renders t in z to the second, or "-" for nil.
func displayOptionalInstant(t *time.Time, z zone) string {
	if t == nil {
		return "-"
	}

	return displayTime(*t, timeline.PrecisionSecond, z)
}
