package cli

import (
	"log/slog"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// logRead logs at debug what a read command read: counts, how many records of each kind it
// returned, after args, the attributes that narrowed the read.
func logRead(logger *slog.Logger, counts slog.Value, args ...any) {
	logger.Debug("records read", append(args, "counts", counts)...)
}

// countOf is the counts of a read that returned n records of one kind.
func countOf(kind string, n int) slog.Value {
	return slog.GroupValue(slog.Int(kind, n))
}

// readEventArgs names the event -e named, events' only one, or nothing when ref is nil.
func readEventArgs(ref *eventRef, events []timeline.Event) []any {
	if ref == nil {
		return nil
	}
	return []any{"event", events[0].ID}
}

// filterValue is what narrowed a read of events under f, never --contains' text. The selected
// timelines and typed times have lines of their own; with nothing else set it is empty, and slog
// leaves it out.
func filterValue(f store.EventFilter) slog.Value {
	var attrs []slog.Attr
	if len(f.Sources) > 0 {
		attrs = append(attrs, slog.Any("sources", f.Sources))
	}
	if len(f.Tags) > 0 {
		attrs = append(attrs, slog.Any("tags", f.Tags))
	}
	if f.Contains != "" {
		attrs = append(attrs, slog.Bool("contains", true))
	}
	if f.Order == store.OrderDesc {
		attrs = append(attrs, slog.String("order", "desc"))
	}
	if f.Limit > 0 {
		attrs = append(attrs, slog.Int("limit", f.Limit))
	}
	return slog.GroupValue(attrs...)
}

// exportCounts is how many records of each kind env holds.
func exportCounts(env export.Envelope) slog.Value {
	return slog.GroupValue(
		slog.Int("timelines", len(env.Timelines)), slog.Int("events", len(env.Events)),
		slog.Int("annotations", len(env.Annotations)), slog.Int("attachments", len(env.Attachments)),
		slog.Int("links", len(env.Links)), slog.Int("sources", len(env.Sources)),
	)
}
