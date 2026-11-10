// Package cli holds coroner's cobra commands and Run, which executes them against an App.
package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
)

// Run executes the command line in app.Args and returns the process exit code. It appends
// to the diagnostic log beside the database the command opens.
func Run(ctx context.Context, app clikit.App) int {
	app, root := newRun(ctx, app)

	return execute(ctx, app, root)
}

// newRun returns app with its diagnostic log watching every database open, and the root
// built over it.
func newRun(ctx context.Context, app clikit.App) (clikit.App, *cobra.Command) {
	log := clikit.NewDiagnosticLog(app)
	app.Log = log
	app.OpenStore = clikit.Watching(app.OpenStore, log, app.Err)
	app.OpenStoreReadOnly = clikit.Watching(app.OpenStoreReadOnly, log, app.Err)
	root := NewRoot(ctx, app)
	log.SetCommand(clikit.CommandOf(root, app.Args))

	return app, root
}

// execute runs root, which NewRoot built from the same ctx and app, and returns the process
// exit code.
func execute(ctx context.Context, app clikit.App, root *cobra.Command) int {
	start := app.Now()
	// Cobra falls back to os.Args when given nil.
	root.SetArgs(append([]string{}, app.Args...))
	root.SetIn(app.In)
	// No SetOut: cobra prints deprecation notices through OutOrStderr, which returns Out
	// once set. TODO: those notices and __complete's output still reach os.Stderr or
	// os.Stdout rather than Err and Out.
	root.SetErr(app.Err)

	cmd, err := root.ExecuteContextC(ctx)
	failure := classify(ctx, cmd, err)

	// Before WriteError: under --json --debug the error envelope must stay stderr's last line.
	app.Log.Finish(ctx, app.Err, cmd, failure, app.Now().Sub(start))

	if failure == nil {
		return 0
	}

	style, _ := clikit.ErrorStyleFor(app)
	// Nowhere is left to report a failed write to Err.
	_ = clikit.WriteError(app.Err, failure, style)

	return failure.Kind.ExitCode()
}

// classify returns the failure behind a finished command line, or nil on success. A
// command's hooks and body return *Error (wrapRunErrors sees to that), so any other error is
// cobra rejecting the command line.
func classify(ctx context.Context, cmd *cobra.Command, err error) *clikit.Error {
	if ctx.Err() != nil {
		return &clikit.Error{Kind: clikit.KindInterrupted, Message: "interrupted", Err: ctx.Err()}
	}

	if err == nil {
		return nil
	}

	if failure, ok := errors.AsType[*clikit.Error](err); ok {
		return failure
	}

	return clikit.UsageError(cmd, err)
}
