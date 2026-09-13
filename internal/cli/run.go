// Package cli holds coroner's cobra commands and Run, which executes them against an App.
package cli

import (
	"context"
	"database/sql"
	"io"
	"time"
)

// App is everything a command may use from outside itself. Commands go through these
// fields, never cobra's InOrStdin, OutOrStdout or ErrOrStderr.
type App struct {
	// Args excludes the program name.
	Args      []string
	In        io.Reader
	Out, Err  io.Writer
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
	// once set. TODO: cobra's own help and usage output still reach os.Stdout/os.Stderr.
	root.SetErr(app.Err)

	if root.ExecuteContext(ctx) != nil {
		return 1
	}

	return 0
}
