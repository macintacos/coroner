package cli

import (
	"context"
	"errors"
	"io/fs"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// records is what the shared lookups read: a *store.Store, or a *store.Tx inside a Change.
type records interface {
	Timeline(ctx context.Context, id string) (timeline.Timeline, error)
	TimelineByName(ctx context.Context, name string) (timeline.Timeline, error)
	CurrentTimeline(ctx context.Context) (timeline.Timeline, error)
	Timelines(ctx context.Context) ([]timeline.Timeline, error)
	OverlayByName(ctx context.Context, name string) (timeline.Overlay, error)
	Event(ctx context.Context, id string) (timeline.Event, error)
	EventBySeq(ctx context.Context, timelineID string, seq int64) (timeline.Event, error)
	EventsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Event, error)
	Annotation(ctx context.Context, id string) (timeline.Annotation, error)
	AnnotationsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Annotation, error)
	AttachmentsByIDPrefix(ctx context.Context, prefix string, limit int) ([]timeline.Attachment, error)
	ListAttachments(ctx context.Context, owner timeline.AttachmentOwner) ([]timeline.Attachment, error)
}

// withStorePath opens the database at path, creating and migrating it as needed, through
// withStoreOpenedBy.
func withStorePath(ctx context.Context, app App, path string, fn func(*store.Store) error) error {
	return withStoreOpenedBy(ctx, app.OpenStore, path, fn)
}

// withChange opens the database at path and runs fn in one Change, returning what fn's last run
// produced and the attachment rows the Change inserted.
func withChange[T any](
	ctx context.Context, app App, path string, opts store.ChangeOptions, fn func(*store.Tx) (T, error),
) (T, []timeline.Attachment, error) {
	var result T
	var inserted []timeline.Attachment
	err := withStorePath(ctx, app, path, func(s *store.Store) error {
		var err error
		inserted, err = s.Change(ctx, opts, func(tx *store.Tx) error {
			var err error
			result, err = fn(tx)
			return err
		})
		return err
	})

	return result, inserted, err
}

// collectBlobs deletes the blobs no attachment references, after a delete commits.
func collectBlobs(ctx context.Context, app App, path string) error {
	return withStorePath(ctx, app, path, func(s *store.Store) error {
		_, err := s.GarbageCollectBlobs(ctx)
		return err
	})
}

// modeFor returns the Change mode --dry-run asks for.
func modeFor(dryRun bool) store.Mode {
	if dryRun {
		return store.Rollback
	}

	return store.Commit
}

// withStoreOpenedBy opens the database at path with open, runs fn against it and closes it.
// Errors from opening the store pass through storeError, and fn's through callbackError.
func withStoreOpenedBy(
	ctx context.Context, open func(context.Context, string) (*store.Store, error), path string, fn func(*store.Store) error,
) error {
	s, err := open(ctx, path)
	if err != nil {
		return storeError(err, path)
	}

	err = callbackError(fn(s), path)
	closeErr := s.Close()
	if closeErr != nil {
		return errors.Join(err, closeErr)
	}

	return err
}

// resolveConfig returns the configuration --db, --config and the environment select, with
// its errors as usage.
func resolveConfig(cmd *cobra.Command, app App) (config.Config, error) {
	flags := cmd.Flags()
	resolved, err := config.Resolve(
		config.Flags{DBPath: changedString(flags, "db"), ConfigPath: changedString(flags, "config")},
		app.LookupEnv, app.ReadFile)
	if err != nil {
		return resolved, &Error{
			Kind: KindUsage, Message: err.Error(), Hint: "Check --db, --config, CORONER_DB and CORONER_CONFIG.", Err: err,
		}
	}

	return resolved, nil
}

// changedString returns the value of the flag called name, or nil when it was not given.
func changedString(flags *pflag.FlagSet, name string) *string {
	if !flags.Changed(name) {
		return nil
	}

	value, _ := flags.GetString(name)
	return &value
}

// callbackError maps an error from work done against an open store: an *Error stays as it is,
// and a permission error names the file it is about rather than the database.
func callbackError(err error, path string) error {
	if _, ok := errors.AsType[*Error](err); ok {
		return err
	}
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok && errors.Is(err, fs.ErrPermission) {
		return &Error{
			Kind: KindDBUnreadable, Message: "permission denied on " + pathErr.Path,
			Hint: "Check the permissions of that path.", Err: err,
		}
	}

	return storeError(err, path)
}

// storeError gives the database failures a user can fix their kinds and messages, gives a
// record the store rejected the kind its first rule names, and returns any other error as it
// is. path is the database withStorePath opened.
func storeError(err error, path string) error {
	switch {
	case errors.Is(err, store.ErrBusy):
		return &Error{
			Kind: KindDBBusy, Message: "the database is busy", Hint: "Another coroner is writing to it; retry shortly.", Err: err,
		}
	case errors.Is(err, store.ErrDatabaseTooNew):
		return &Error{
			Kind: KindDBTooNew, Message: "the database was written by a newer coroner", Hint: "Upgrade coroner to use it.", Err: err,
		}
	case errors.Is(err, store.ErrForeignDatabase):
		return &Error{
			Kind: KindDBUnreadable, Message: path + " is not a coroner database",
			Hint: "Pass --db with the path to a coroner database.", Err: err,
		}
	case errors.Is(err, store.ErrBackupExists):
		// The store's text names the backup and how to clear it.
		return &Error{Kind: KindDBUnreadable, Message: err.Error(), Err: err}
	case errors.Is(err, fs.ErrPermission):
		return &Error{
			Kind: KindDBUnreadable, Message: "permission denied opening the database at " + path,
			Hint: "Check --db and CORONER_DB, or the permissions of that path.", Err: err,
		}
	}

	return recordError(err, "")
}
