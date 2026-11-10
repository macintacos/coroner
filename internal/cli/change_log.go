package cli

import (
	"context"
	"log/slog"
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
