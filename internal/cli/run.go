// Package cli holds coroner's cobra commands and Run, which executes them against an App.
package cli

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
)

// App is everything a command may use from outside itself. Commands go through these
// fields, never cobra's InOrStdin, OutOrStdout or ErrOrStderr.
type App struct {
	// Args excludes the program name.
	Args               []string
	In                 io.Reader
	Out, Err           io.Writer
	OutIsTTY, ErrIsTTY bool
	// Version is the build's module version, as debug.ReadBuildInfo reports it.
	Version   string
	LookupEnv func(key string) (string, bool)
	// ReadFile must report a missing file as fs.ErrNotExist, as os.ReadFile does.
	ReadFile func(name string) ([]byte, error)
	Now      func() time.Time
	// OpenStore creates the database at path, so only commands that use it may call it, and
	// never from PersistentPreRunE.
	OpenStore func(ctx context.Context, path string) (*sql.DB, error)
}

// Run executes the command line in app.Args and returns the process exit code.
func Run(ctx context.Context, app App) int {
	root := newRoot(app)
	// Cobra falls back to os.Args when given nil.
	root.SetArgs(append([]string{}, app.Args...))
	root.SetIn(app.In)
	// No SetOut: cobra prints deprecation notices through OutOrStderr, which returns Out
	// once set. TODO: those notices, `help <unknown>` and __complete's output still reach
	// os.Stderr or os.Stdout rather than Err and Out.
	root.SetErr(app.Err)

	cmd, err := root.ExecuteContextC(ctx)
	failure := classify(ctx, cmd, err)
	if failure == nil {
		return 0
	}

	code := failure.Kind.ExitCode()
	style := errorStyleFor(app)
	if style.debug {
		logger := slog.New(slog.NewTextHandler(app.Err, &slog.HandlerOptions{Level: slog.LevelDebug}))
		logger.DebugContext(ctx, "command failed",
			"command", cmd.CommandPath(), "kind", failure.Kind, "exit_code", code, "error", failure.Err)
	}

	// Nowhere is left to report a failed write to Err.
	_ = writeError(app.Err, failure, style)

	return code
}

// classify returns the failure behind a finished command line, or nil on success. Commands
// return *Error (wrapRunErrors sees to that), so any other error is cobra rejecting the
// command line.
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

// errorStyleFor reads the global flags that shape error output straight from app.Args,
// because cobra may have failed before parsing them.
func errorStyleFor(app App) errorStyle {
	flags := globalFlags()
	flags.ParseErrorsAllowlist.UnknownFlags = true
	flags.SetOutput(io.Discard)
	// ponytail: a subcommand flag whose value starts with "--" (--text --json) reads here as
	// the next flag; parse with the failing command's own flag set if that ever matters.
	_ = flags.Parse(app.Args)

	format, _ := flags.GetString("format")
	json, _ := flags.GetBool("json")
	debug, _ := flags.GetBool("debug")
	noColor, _ := flags.GetBool("no-color")

	return errorStyle{
		color:   colorEnabled(app.ErrIsTTY, noColor, app.LookupEnv),
		debug:   debug || envSet(app.LookupEnv, "CORONER_DEBUG"),
		machine: json || format == "json" || format == "jsonl",
	}
}

// colorEnabled reports whether a stream may carry color.
func colorEnabled(isTTY, noColorFlag bool, lookupEnv func(string) (string, bool)) bool {
	term, _ := lookupEnv("TERM")
	return isTTY && !noColorFlag && term != "dumb" &&
		!envSet(lookupEnv, "NO_COLOR") && !envSet(lookupEnv, "CORONER_NO_COLOR")
}

// envSet reports whether the environment variable key is set and non-empty.
func envSet(lookupEnv func(string) (string, bool), key string) bool {
	value, _ := lookupEnv(key)
	return value != ""
}
