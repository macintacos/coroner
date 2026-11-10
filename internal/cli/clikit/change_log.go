package clikit

import (
	"context"
	"log/slog"
	"slices"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

// WithDryRun returns logger, adding dry_run to every line when --dry-run rolled the change back.
func WithDryRun(logger *slog.Logger, dryRun bool) *slog.Logger {
	if !dryRun {
		return logger
	}
	return logger.With("dry_run", true)
}

// LogCounted logs, when ids is not empty, msg at info with how many there are, then msg at
// debug once per id, under key.
func LogCounted(ctx context.Context, logger *slog.Logger, msg, key string, ids []string) {
	if len(ids) == 0 {
		return
	}
	logger.InfoContext(ctx, msg, "count", len(ids))
	for _, id := range ids {
		logger.DebugContext(ctx, msg, key, id)
	}
}

// Sweep is the records a delete removes, or changes, beside the records it names, by id.
type Sweep struct {
	Events, Annotations, Attachments, Links, Overlays []string
}

// Log logs each kind's count at info and its ids at debug.
func (s Sweep) Log(ctx context.Context, logger *slog.Logger) {
	LogCounted(ctx, logger, "events deleted", "event", s.Events)
	LogCounted(ctx, logger, "annotations deleted", "annotation", s.Annotations)
	LogCounted(ctx, logger, "attachments deleted", "attachment", s.Attachments)
	LogCounted(ctx, logger, "links deleted", "link", s.Links)
	LogCounted(ctx, logger, "overlays changed", "overlay_id", s.Overlays)
}

// IDsOf returns the id of each of records, in order.
func IDsOf[T any](records []T, id func(T) string) []string {
	ids := make([]string, 0, len(records))
	for _, r := range records {
		ids = append(ids, id(r))
	}
	return ids
}

// EventID returns e's id, for IDsOf.
func EventID(e timeline.Event) string { return e.ID }

// AnnotationID returns a's id, for IDsOf.
func AnnotationID(a timeline.Annotation) string { return a.ID }

// AttachmentID returns a's id, for IDsOf.
func AttachmentID(a timeline.Attachment) string { return a.ID }

// OverlayID returns o's id, for IDsOf.
func OverlayID(o timeline.Overlay) string { return o.ID }

// The export keys of the times an edit or a replay moves on its own.
const (
	KeyCreatedAt = "created_at"
	KeyUpdatedAt = "updated_at"
)

// IDKey returns the log key for the id of an export record of type typ: timeline_id and
// source_id, since timeline and source carry names, and typ itself otherwise.
func IDKey(typ string) string {
	switch typ {
	case "timeline", "source":
		return typ + "_id"
	}
	return typ
}

// ChangedFields returns the export keys whose values differ between before and after, without
// updated_at, which every edit moves. It is never nil, so an edit that changed nothing logs [].
func ChangedFields[T export.Record](before, after T) []string {
	keys := slices.DeleteFunc(export.DifferingKeys(before, after), func(key string) bool { return key == KeyUpdatedAt })
	return append([]string{}, keys...)
}

// AnnotationArgs returns the keys naming an annotation, its timeline and, when on one, its event.
func AnnotationArgs(a timeline.Annotation) []any {
	args := []any{"annotation", a.ID, "timeline_id", a.TimelineID}
	if a.EventID != nil {
		args = append(args, "event", *a.EventID)
	}
	return args
}

// OwnerArgs returns the key and id of the event or annotation an attachment belongs to.
func OwnerArgs(owner timeline.AttachmentOwner) []any {
	if owner.EventID != nil {
		return []any{"event", *owner.EventID}
	}
	return []any{"annotation", *owner.AnnotationID}
}
