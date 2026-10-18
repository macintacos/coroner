package cli

import (
	"context"
	"errors"
	"io/fs"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

// withStore opens the database the configuration selects, through resolveConfig and
// withStorePath.
func withStore(ctx context.Context, cmd *cobra.Command, app App, fn func(*store.Store) error) error {
	resolved, err := resolveConfig(cmd, app)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, resolved.DBPath, fn)
}

// withStorePath opens the database at path, runs fn against it and closes it. Errors from
// opening the store or from fn pass through storeError.
func withStorePath(ctx context.Context, app App, path string, fn func(*store.Store) error) error {
	s, err := app.OpenStore(ctx, path)
	if err != nil {
		return storeError(err, path)
	}

	err = storeError(fn(s), path)
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

// storeError gives the database failures a user can fix their kinds and messages, and
// returns any other error as it is. path is the database withStorePath opened.
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

	return err
}
