// Package cli holds coroner's cobra commands and the process-free entry point that runs them.
package cli

import (
	"context"
	"database/sql"
	"io"
	"time"
)

// App is everything a command may reach outside the process: main.go builds it from the
// real args, stdio, environment, clock and store.
type App struct {
	Args      []string
	In        io.Reader
	Out, Err  io.Writer
	LookupEnv func(key string) (string, bool)
	Now       func() time.Time
	// OpenStore is called only by commands that need the database, so --help and friends
	// never create one.
	OpenStore func(ctx context.Context, path string) (*sql.DB, error)
}

// Run executes the command line in app.Args and returns the process exit code.
func Run(ctx context.Context, app App) int {
	root := newRoot(app)
	// Cobra falls back to os.Args when given nil.
	root.SetArgs(append([]string{}, app.Args...))
	root.SetIn(app.In)
	root.SetErr(app.Err)

	if root.ExecuteContext(ctx) != nil {
		return 1
	}

	return 0
}
