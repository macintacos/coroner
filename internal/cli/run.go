// Package cli holds coroner's cobra commands and Run, which executes them against an App.
package cli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
)

// App is everything a command may use from outside itself. Commands go through these
// fields, never cobra's InOrStdin, OutOrStdout or ErrOrStderr.
type App struct {
	// Args excludes the program name.
	Args     []string
	In       io.Reader
	Out, Err io.Writer
	// A command never waits on a terminal In.
	InIsTTY bool
	// Color and layout are decided per stream.
	OutIsTTY, ErrIsTTY bool
	// Version is the build's module version, as debug.ReadBuildInfo reports it.
	Version   string
	LookupEnv func(key string) (string, bool)
	// ReadFile must report a missing file as fs.ErrNotExist, as os.ReadFile does.
	ReadFile func(name string) ([]byte, error)
	// WriteFile creates or truncates the file at name, as os.WriteFile does.
	WriteFile func(name string, data []byte, perm fs.FileMode) error
	// WriteNewFile creates name and writes data to it, failing with an error that wraps
	// fs.ErrExist, and writing nothing, when name already exists.
	WriteNewFile func(name string, data []byte, perm fs.FileMode) error
	// Now returns the time in the local zone, which is where time input with no zone is read
	// when neither --tz nor the config file names one.
	Now func() time.Time
	// NewID returns the ID for a new record, a UUIDv7 in main.go.
	NewID func() (uuid.UUID, error)
	// OpenStore creates the database at path, so only commands that use it may call it, and
	// never from PersistentPreRunE.
	OpenStore func(ctx context.Context, path string) (*store.Store, error)
	// OpenStoreReadOnly opens the database at path without creating the database, migrating or
	// writing it, and must report a missing file as fs.ErrNotExist.
	OpenStoreReadOnly func(ctx context.Context, path string) (*store.Store, error)
}

// Run executes the command line in app.Args and returns the process exit code.
func Run(ctx context.Context, app App) int {
	root := NewRoot(ctx, app)
	// Cobra falls back to os.Args when given nil.
	root.SetArgs(append([]string{}, app.Args...))
	root.SetIn(app.In)
	// No SetOut: cobra prints deprecation notices through OutOrStderr, which returns Out
	// once set. TODO: those notices and __complete's output still reach os.Stderr or
	// os.Stdout rather than Err and Out.
	root.SetErr(app.Err)

	cmd, err := root.ExecuteContextC(ctx)
	failure := classify(ctx, cmd, err)
	if failure == nil {
		return 0
	}

	style, debug := errorStyleFor(app)
	if debug {
		logFailure(ctx, app, cmd, failure)
	}

	// Nowhere is left to report a failed write to Err.
	_ = writeError(app.Err, failure, style)

	return failure.Kind.ExitCode()
}

// classify returns the failure behind a finished command line, or nil on success. A
// command's hooks and body return *Error (wrapRunErrors sees to that), so any other error is
// cobra rejecting the command line.
func classify(ctx context.Context, cmd *cobra.Command, err error) *Error {
	if ctx.Err() != nil {
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: ctx.Err()}
	}

	if err == nil {
		return nil
	}

	if failure, ok := errors.AsType[*Error](err); ok {
		return failure
	}

	return usageError(cmd, err)
}

// logFailure writes failure as a debug log line on app.Err.
func logFailure(ctx context.Context, app App, cmd *cobra.Command, failure *Error) {
	attrs := []any{
		"command", cmd.CommandPath(), "kind", failure.Kind, "exit_code", failure.Kind.ExitCode(),
		"message", failure.Message, "args", app.Args,
	}
	if failure.Err != nil {
		attrs = append(attrs, "error", failure.Err)
	}

	logger := slog.New(slog.NewTextHandler(app.Err, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.DebugContext(ctx, "command failed", attrs...)
}
