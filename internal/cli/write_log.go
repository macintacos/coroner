package cli

import "log/slog"

// logFileWritten logs at info a file a command wrote outside the database, size bytes at path;
// stdout, -, is not a file.
func logFileWritten(logger *slog.Logger, path string, size int, attrs ...slog.Attr) {
	if path == "-" {
		return
	}
	args := []any{slog.String("path", path), slog.Int("size", size)}
	for _, a := range attrs {
		args = append(args, a)
	}
	logger.Info("file written", args...)
}
