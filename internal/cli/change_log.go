package cli

import (
	"context"
	"log/slog"
	"slices"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

// withDryRun returns logger, adding dry_run to every line when --dry-run rolled the change back.
func withDryRun(logger *slog.Logger, dryRun bool) *slog.Logger {
	if !dryRun {
		return logger
	}
	return logger.With("dry_run", true)
}

// logCounted logs, when ids is not empty, msg at info with how many there are, then msg at
// debug once per id, under key.
func logCounted(ctx context.Context, logger *slog.Logger, msg, key string, ids []string) {
	if len(ids) == 0 {
		return
	}
	logger.InfoContext(ctx, msg, "count", len(ids))
	for _, id := range ids {
		logger.DebugContext(ctx, msg, key, id)
	}
}

// idKey returns rule 5's name for the id of an export record type: timeline_id and
// source_id for the named ones, the type itself for the rest.
func idKey(typ string) string {
	switch typ {
	case "timeline", "source":
		return typ + "_id"
	}
	return typ
}

// changedFields returns the export keys whose values differ between before and after, without
// updated_at, which every edit moves. It is never nil, so an edit that changed nothing logs [].
func changedFields[T export.Record](before, after T) []string {
	keys := slices.DeleteFunc(export.DifferingKeys(before, after), func(key string) bool { return key == "updated_at" })
	return append([]string{}, keys...)
}

// annotationArgs returns the keys naming an annotation, its timeline and, when on one, its event.
func annotationArgs(a timeline.Annotation) []any {
	args := []any{"annotation", a.ID, "timeline_id", a.TimelineID}
	if a.EventID != nil {
		args = append(args, "event", *a.EventID)
	}
	return args
}

// ownerArgs returns the key and id of the event or annotation an attachment belongs to.
func ownerArgs(owner timeline.AttachmentOwner) []any {
	if owner.EventID != nil {
		return []any{"event", *owner.EventID}
	}
	return []any{"annotation", *owner.AnnotationID}
}
