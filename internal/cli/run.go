// Package cli holds coroner's cobra commands and Run, which executes them against an App.
package cli

import (
	"cmp"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
	Version string
	// Skill is skills.Coroner, the agent skill's SKILL.md, which `agent skill` prints.
	Skill     string
	LookupEnv func(key string) (string, bool)
	// ReadFile must report a missing file as fs.ErrNotExist, as os.ReadFile does.
	ReadFile func(name string) ([]byte, error)
	// WriteFile creates or truncates the file at name, as os.WriteFile does.
	WriteFile func(name string, data []byte, perm fs.FileMode) error
	// OpenFile opens name as os.OpenFile does.
	OpenFile func(name string, flag int, perm fs.FileMode) (io.WriteCloser, error)
	// RemoveFile removes name as os.Remove does.
	RemoveFile func(name string) error
	// ReadDir lists the directory at name as os.ReadDir does, reporting a missing one as
	// fs.ErrNotExist.
	ReadDir func(name string) ([]fs.DirEntry, error)
	// Stat describes the file at name as os.Stat does.
	Stat func(name string) (fs.FileInfo, error)
	// MkdirAll creates the directory at path and any missing parents, as os.MkdirAll does.
	MkdirAll func(path string, perm fs.FileMode) error
	// Now returns the time in the local zone, which --tz local and default_timezone = "local"
	// name.
	Now func() time.Time
	// NewID returns the ID for a new record, a UUIDv7 in main.go.
	NewID func() (uuid.UUID, error)
	// OpenStore creates the database at path, so only commands that use it may call it, and
	// never from PersistentPreRunE.
	OpenStore func(ctx context.Context, path string) (*store.Store, error)
	// OpenStoreReadOnly opens the database at path without creating the database, migrating or
	// writing it, and must report a missing file as fs.ErrNotExist.
	OpenStoreReadOnly func(ctx context.Context, path string) (*store.Store, error)
	// Rename renames oldpath to newpath, replacing newpath, as os.Rename does.
	Rename func(oldpath, newpath string) error

	// log is the command's diagnostic log; only Run sets it.
	log *diagnosticLog
}

// Run executes the command line in app.Args and returns the process exit code.
func Run(ctx context.Context, app App) int {
	log := newDiagnosticLog(app)
	app.log = log
	app.OpenStore = watching(app.OpenStore, log)
	app.OpenStoreReadOnly = watching(app.OpenStoreReadOnly, log)
	root := NewRoot(ctx, app)
	log.command, log.off = commandOf(root, app.Args)

	return execute(ctx, app, root)
}

// execute runs root, which NewRoot built from the same ctx and app, and returns the process
// exit code.
func execute(ctx context.Context, app App, root *cobra.Command) int {
	// Cobra falls back to os.Args when given nil.
	root.SetArgs(append([]string{}, app.Args...))
	root.SetIn(app.In)
	// No SetOut: cobra prints deprecation notices through OutOrStderr, which returns Out
	// once set. TODO: those notices and __complete's output still reach os.Stderr or
	// os.Stdout rather than Err and Out.
	root.SetErr(app.Err)

	cmd, err := root.ExecuteContextC(ctx)
	failure := classify(ctx, cmd, err)

	log := cmp.Or(app.log, newDiagnosticLog(app))
	logger := log.logger(app.Err)
	kind, code := Kind("ok"), 0
	if failure != nil {
		logFailure(ctx, logger, cmd, failure)
		kind, code = failure.Kind, failure.Kind.ExitCode()
	}
	logger.InfoContext(ctx, "command finished", "kind", kind, "exit_code", code, "duration", log.elapsed())
	log.close()

	if failure == nil {
		return 0
	}

	style, _ := errorStyleFor(app)
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

// logFailure logs failure as a debug line.
func logFailure(ctx context.Context, logger *slog.Logger, cmd *cobra.Command, failure *Error) {
	attrs := []any{
		"kind", failure.Kind, "exit_code", failure.Kind.ExitCode(),
		"message", failure.Message, "flags", flagsGiven(cmd),
	}
	if failure.Err != nil {
		attrs = append(attrs, "error", failure.Err)
	}

	logger.DebugContext(ctx, "command failed", attrs...)
}

// flagsGiven returns the names of the flags set on cmd's command line, never their values.
func flagsGiven(cmd *cobra.Command) []string {
	var names []string
	cmd.Flags().Visit(func(flag *pflag.Flag) { names = append(names, flag.Name) })

	return names
}
