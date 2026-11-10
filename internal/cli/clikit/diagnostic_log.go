package clikit

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

// LogRotateSize is the size past which a command that opens the diagnostic log first moves it
// to <log>.1.
const LogRotateSize = 50_000_000

// DiagnosticLogAnnotation set to "off" on a command keeps it, and every command under it, out
// of the diagnostic log.
const DiagnosticLogAnnotation = "diagnostic-log"

// DiagnosticLog is one command's diagnostic log: JSON lines held until the command opens a
// database that exists, then appended to the log beside it, one write per line. No error it
// meets reaches the command.
type DiagnosticLog struct {
	Stat   func(name string) (fs.FileInfo, error)
	Open   func(name string, flag int, perm fs.FileMode) (io.WriteCloser, error)
	Rename func(oldpath, newpath string) error
	Limit  int64
	Now    func() time.Time
	Debug  bool

	mu          sync.Mutex
	commandPath string
	held        [][]byte
	file        io.WriteCloser
	done        bool // the log was opened, failed to open, or closed: never try again
}

// NewDiagnosticLog returns app's diagnostic log, which opens no file until Opened is called.
func NewDiagnosticLog(app App) *DiagnosticLog {
	_, debug := ErrorStyleFor(app)
	return &DiagnosticLog{
		Stat: app.Stat, Open: app.OpenFile, Rename: app.Rename, Limit: LogRotateSize, Now: app.Now, Debug: debug,
	}
}

// Write holds a copy of p, or appends it to the open log in one write. It always reports
// len(p), nil: slog's JSON handler calls it once per line.
func (d *DiagnosticLog) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case d.file != nil:
		_, _ = d.file.Write(p)
	case !d.done:
		d.held = append(d.held, append([]byte(nil), p...))
	}

	return len(p), nil
}

// SetCommand names the command every later logger's lines carry, and, when off, drops every
// line held or written from now on.
func (d *DiagnosticLog) SetCommand(path string, off bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.commandPath = path
	if off {
		d.held, d.done = nil, true
	}
}

// Lines returns a copy of the lines held until a log opens.
func (d *DiagnosticLog) Lines() [][]byte {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.held)
}

// Opened is told the path of every database the command tried to open and what the open
// returned. The first that exists and is not foreign opens the log beside it, rotated, and
// writes the held lines.
func (d *DiagnosticLog) Opened(dbPath string, openErr error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.done || errors.Is(openErr, store.ErrForeignDatabase) {
		return
	}
	_, err := d.Stat(dbPath)
	if err != nil {
		return
	}

	d.done = true
	held := d.held
	d.held = nil
	file, err := d.openLog(config.DiagnosticLogPath(dbPath))
	if err != nil {
		return // the log never fails the command
	}
	d.file = file
	for _, line := range held {
		_, _ = file.Write(line)
	}
}

// openLog moves path to path+".1" when it is over d.Limit, then opens it for appending,
// owner-only.
func (d *DiagnosticLog) openLog(path string) (io.WriteCloser, error) {
	info, err := d.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil && info.Size() > d.Limit {
		err = d.Rename(path, path+".1")
		if err != nil {
			return nil, err
		}
	}

	return d.Open(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
}

// Close closes the log, drops held lines, and drops every later write, such as one from a
// viewer goroutine still running.
func (d *DiagnosticLog) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.file != nil {
		_ = d.file.Close()
	}
	d.file, d.held, d.done = nil, nil, true
}

// Finish logs the command's failure, under debug, and its finish line, then closes the log.
func (d *DiagnosticLog) Finish(ctx context.Context, stderr io.Writer, cmd *cobra.Command, failure *Error, duration time.Duration) {
	logger := d.Logger(stderr)
	kind, code := "ok", 0
	if failure != nil {
		logFailure(ctx, logger, cmd, failure)
		kind, code = string(failure.Kind), failure.Kind.ExitCode()
	}
	logger.InfoContext(ctx, "command finished", "kind", kind, "exit_code", code, "duration", duration)
	d.Close()
}

// Logger returns the command's diagnostic logger, or one that discards every line for an App
// newRun did not build.
func (app App) Logger() *slog.Logger {
	if app.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return app.Log.Logger(app.Err)
}

// Logger returns a logger whose JSON lines go to d and, under debug, whose text lines go to
// stderr as they are logged. Every line carries the command.
func (d *DiagnosticLog) Logger(stderr io.Writer) *slog.Logger {
	// slog stamps records with the real clock, which the App's clock must replace.
	replaceTime := func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Time(slog.TimeKey, d.Now())
		}
		return a
	}
	level := slog.LevelInfo
	if d.Debug {
		level = slog.LevelDebug
	}
	handlers := []slog.Handler{slog.NewJSONHandler(d, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceTime})}
	if d.Debug {
		handlers = append(handlers, slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceTime}))
	}

	d.mu.Lock()
	command := d.commandPath
	d.mu.Unlock()

	return slog.New(slog.NewMultiHandler(handlers...)).With("command", command)
}

// Watching returns open, telling log the path of each database it tries to open. It hands
// open the command's logger, built at open time so it carries the command path, and the
// command's clock, before any options the caller gives, so the caller's win.
func Watching(
	open func(ctx context.Context, path string, opts ...store.Option) (*store.Store, error),
	log *DiagnosticLog, stderr io.Writer,
) func(ctx context.Context, path string, opts ...store.Option) (*store.Store, error) {
	return func(ctx context.Context, path string, opts ...store.Option) (*store.Store, error) {
		s, err := open(ctx, path, append([]store.Option{store.WithLogger(log.Logger(stderr)), store.WithClock(log.Now)}, opts...)...)
		log.Opened(path, err)
		return s, err
	}
}

// CommandOf returns the path of the command args run, found as cobra's ExecuteC finds it, and
// whether it stays out of the diagnostic log. Cobra adds __complete only while executing, so a
// completion request is told by its first argument, as cobra tells it.
func CommandOf(root *cobra.Command, args []string) (string, bool) {
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		return root.CommandPath() + " " + args[0], true
	}

	cmd, _, err := root.Find(args)
	if err != nil {
		cmd = root
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[DiagnosticLogAnnotation] == "off" {
			return cmd.CommandPath(), true
		}
	}

	return cmd.CommandPath(), false
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
