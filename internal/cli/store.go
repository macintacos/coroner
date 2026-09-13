package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
)

// withStore opens the database that --db, --config and the environment select, runs fn
// against it and closes it.
func withStore(ctx context.Context, cmd *cobra.Command, app App, fn func(*store.Store) error) error {
	flags := cmd.Flags()
	resolved, err := config.Resolve(
		config.Flags{DBPath: changedString(flags, "db"), ConfigPath: changedString(flags, "config")},
		app.LookupEnv, app.ReadFile)
	if err != nil {
		return &Error{
			Kind: KindUsage, Message: err.Error(), Hint: "Check --db, --config, CORONER_DB and CORONER_CONFIG.", Err: err,
		}
	}

	s, err := app.OpenStore(ctx, resolved.DBPath)
	if err != nil {
		return storeError(err)
	}

	err = storeError(fn(s))
	closeErr := s.Close()
	if closeErr != nil {
		return errors.Join(err, closeErr)
	}

	return err
}

// changedString returns the value of the flag called name, or nil when it was not given.
func changedString(flags *pflag.FlagSet, name string) *string {
	if !flags.Changed(name) {
		return nil
	}

	value, _ := flags.GetString(name)
	return &value
}

// storeError gives the store's database-level failures their kinds and returns any other
// error as it is.
func storeError(err error) error {
	switch {
	case errors.Is(err, store.ErrBusy):
		return &Error{
			Kind: KindDBBusy, Message: err.Error(), Hint: "Another coroner is writing to the database; retry shortly.", Err: err,
		}
	case errors.Is(err, store.ErrDatabaseTooNew):
		return &Error{Kind: KindDBTooNew, Message: err.Error(), Hint: "Upgrade coroner to use this database.", Err: err}
	case errors.Is(err, store.ErrForeignDatabase), errors.Is(err, store.ErrBackupExists):
		return &Error{Kind: KindDBUnreadable, Message: err.Error(), Hint: "Pass --db with the path to a coroner database.", Err: err}
	}

	return err
}
