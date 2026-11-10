package cli

import (
	"log/slog"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// logRead logs a read command's records read line at debug: attrs, such as its filter or the
// record it names, then counts, how many records of each kind it read.
func logRead(logger *slog.Logger, counts slog.Value, attrs ...slog.Attr) {
	args := make([]any, 0, len(attrs)+1)
	for _, a := range attrs {
		args = append(args, a)
	}
	logger.Debug("records read", append(args, slog.Any("counts", counts))...)
}

// countOf is the counts of a read that returned n records of one kind.
func countOf(kind string, n int) slog.Value {
	return slog.GroupValue(slog.Int(kind, n))
}

// eventAttrs names the event ref names, or nothing when ref is nil. It relies on readAnnotations
// and readAttachments returning exactly the event ref names when ref is set.
func eventAttrs(ref *eventRef, events []timeline.Event) []slog.Attr {
	if ref == nil {
		return nil
	}
	return []slog.Attr{slog.String("event", events[0].ID)}
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
