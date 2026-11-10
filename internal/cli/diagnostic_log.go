package cli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

// logRotateSize is the size past which a command that opens the diagnostic log first moves it
// to <log>.1.
const logRotateSize = 50 << 20

// diagnosticLogAnnotation set to "off" on a command keeps it, and every command under it, out
// of the diagnostic log.
const diagnosticLogAnnotation = "diagnostic-log"

// diagnosticLog is one command's diagnostic log: JSON lines held until the command opens a
// database that exists, then appended to the log beside it, one write per line. No error it
// meets reaches the command.
type diagnosticLog struct {
	stat    func(name string) (fs.FileInfo, error)
	open    func(name string, flag int, perm fs.FileMode) (io.WriteCloser, error)
	rename  func(oldpath, newpath string) error
	limit   int64
	now     func() time.Time
	start   time.Time
	debug   bool
	command string // the command path, set by Run once the tree is built
	off     bool   // the command opted out: the file gets nothing

	mu   sync.Mutex
	held [][]byte
	file io.WriteCloser
	done bool // the log was opened, failed to open, or closed: never try again
}

// newDiagnosticLog returns app's diagnostic log, which opens no file until opened is called.
func newDiagnosticLog(app App) *diagnosticLog {
	_, debug := errorStyleFor(app)
	d := &diagnosticLog{
		stat: app.Stat, open: app.OpenFile, rename: app.Rename, limit: logRotateSize, now: app.Now, debug: debug,
	}
	if app.Now != nil {
		d.start = app.Now()
	}

	return d
}

// Write holds a copy of p, or appends it to the open log in one write. It always reports
// len(p), nil: slog's JSON handler calls it once per line.
func (d *diagnosticLog) Write(p []byte) (int, error) {
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

// opened is told the path of every database the command tried to open. The first that exists
// opens the log beside it, rotated, and writes the held lines.
func (d *diagnosticLog) opened(dbPath string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// An App built without the file seams, as many specs build one, keeps no log.
	if d.done || d.off || d.stat == nil || d.open == nil || d.rename == nil {
		return
	}
	_, err := d.stat(dbPath)
	if err != nil {
		return
	}

	d.done = true
	d.file = d.openLog(config.LogPath(dbPath))
	if d.file != nil {
		for _, line := range d.held {
			_, _ = d.file.Write(line)
		}
	}
	d.held = nil
}

// openLog moves path to path+".1" when it is over d.limit, then opens it for appending,
// owner-only. It returns nil when anything fails.
func (d *diagnosticLog) openLog(path string) io.WriteCloser {
	info, err := d.stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil && info.Size() > d.limit && d.rename(path, path+".1") != nil {
		return nil
	}

	file, err := d.open(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil
	}

	return file
}

// close closes the log, drops held lines, and drops every later write, such as one from a
// viewer goroutine still running.
func (d *diagnosticLog) close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.file != nil {
		_ = d.file.Close()
	}
	d.file, d.held, d.done = nil, nil, true
}

// elapsed returns the time since the command started, by the App's clock.
func (d *diagnosticLog) elapsed() time.Duration {
	if d.now == nil {
		return 0
	}

	return d.now().Sub(d.start)
}

// logger returns a logger whose JSON lines go to d and, under debug, whose text lines go to
// stderr as they are logged. Every line carries the command.
func (d *diagnosticLog) logger(stderr io.Writer) *slog.Logger {
	// slog stamps records with the real clock, which the App's clock must replace.
	replaceTime := func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey && d.now != nil {
			return slog.Time(slog.TimeKey, d.now())
		}
		return a
	}
	level := slog.LevelInfo
	if d.debug {
		level = slog.LevelDebug
	}
	handlers := []slog.Handler{slog.NewJSONHandler(d, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceTime})}
	if d.debug {
		handlers = append(handlers, slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceTime}))
	}

	return slog.New(slog.NewMultiHandler(handlers...)).With("command", d.command)
}

// watching returns open, telling d the path of each database it tries to open.
func watching(
	open func(ctx context.Context, path string) (*store.Store, error), d *diagnosticLog,
) func(ctx context.Context, path string) (*store.Store, error) {
	return func(ctx context.Context, path string) (*store.Store, error) {
		s, err := open(ctx, path)
		d.opened(path)
		return s, err
	}
}

// commandOf returns the path of the command args run, found as cobra's ExecuteC finds it, and
// whether it stays out of the diagnostic log. Cobra adds __complete only while executing, so a
// completion request is told by its first argument, as cobra tells it.
func commandOf(root *cobra.Command, args []string) (string, bool) {
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		return root.CommandPath() + " " + args[0], true
	}

	cmd, _, err := root.Find(args)
	if err != nil {
		cmd = root
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[diagnosticLogAnnotation] == "off" {
			return cmd.CommandPath(), true
		}
	}

	return cmd.CommandPath(), false
}
