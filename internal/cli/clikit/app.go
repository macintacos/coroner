package clikit

import (
	"context"
	"io"
	"io/fs"
	"time"

	"github.com/google/uuid"

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
	// Open opens name for reading, as os.Open does, reporting a missing file as fs.ErrNotExist.
	Open func(name string) (io.ReadCloser, error)
	// RemoveFile removes name as os.Remove does.
	RemoveFile func(name string) error
	// ReadDir lists the directory at name as os.ReadDir does, reporting a missing one as
	// fs.ErrNotExist.
	ReadDir func(name string) ([]fs.DirEntry, error)
	// Stat describes the file at name as os.Stat does.
	Stat func(name string) (fs.FileInfo, error)
	// MkdirAll creates the directory at path and any missing parents, as os.MkdirAll does.
	MkdirAll func(path string, perm fs.FileMode) error
	// Rename renames oldpath to newpath, replacing newpath, as os.Rename does.
	Rename func(oldpath, newpath string) error
	// Now returns the time in the local zone, which --tz local and default_timezone = "local"
	// name.
	Now func() time.Time
	// NewID returns the ID for a new record, a UUIDv7 in main.go.
	NewID func() (uuid.UUID, error)
	// OpenStore creates the database at path, so only commands that use it may call it, and
	// never from PersistentPreRunE. Under Run, the command's logger and clock come before any
	// options the caller gives, so the caller's win.
	OpenStore func(ctx context.Context, path string, opts ...store.Option) (*store.Store, error)
	// OpenStoreReadOnly opens the database at path without creating the database, migrating or
	// writing it, and must report a missing file as fs.ErrNotExist. Under Run, the command's
	// logger and clock come before any options the caller gives, so the caller's win.
	OpenStoreReadOnly func(ctx context.Context, path string, opts ...store.Option) (*store.Store, error)

	// Log is the command's diagnostic log, set by newRun.
	Log *DiagnosticLog
}
