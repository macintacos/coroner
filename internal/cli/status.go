package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
)

func newStatusCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the database and the current timeline",
		Long: "Show the database coroner uses, its schema version, and the timeline commands act on\n" +
			"with what selected it: -t, CORONER_TIMELINE or 'coroner timeline use'. status never\n" +
			"creates or migrates the database. Its JSON fields are experimental until 1.0.",
		GroupID:     additionalCommands,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner status\n" +
			"  $ CORONER_TIMELINE=mongo-outage-2026-09-11 coroner status --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return showStatus(ctx, cmd, app)
		},
	}

	addTimelineFlag(cmd.Flags())

	return cmd
}

type statusJSON struct {
	DBPath   string `json:"db_path"`
	DBExists bool   `json:"db_exists"`
	// The pointers encode as null when there is no database or no timeline is selected.
	SchemaVersion *int64           `json:"schema_version"`
	Timeline      *timelineRefJSON `json:"timeline"`
	SelectedBy    *selector        `json:"selected_by"`
}

type timelineRefJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func showStatus(ctx context.Context, cmd *cobra.Command, app App) error {
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	resolved, err := resolveConfig(cmd, app)
	if err != nil {
		return err
	}

	s, err := app.OpenStoreReadOnly(ctx, resolved.DBPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return writeStatus(o, statusJSON{DBPath: resolved.DBPath})
	case err != nil:
		return storeError(err, resolved.DBPath)
	}

	report, err := readStatus(ctx, s, changedString(cmd.Flags(), "timeline"), app.LookupEnv)
	err = storeError(err, resolved.DBPath)
	closeErr := s.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}

	report.DBPath = resolved.DBPath
	return writeStatus(o, report)
}

// readStatus reads the schema version of the database s, and the timeline flag,
// CORONER_TIMELINE or the current timeline selects.
func readStatus(
	ctx context.Context, s *store.Store, flag *string, lookupEnv func(string) (string, bool),
) (statusJSON, error) {
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		return statusJSON{}, err
	}
	report := statusJSON{DBExists: true, SchemaVersion: &version}
	// A file without a schema has no tables to select a timeline from.
	if version == 0 {
		return report, nil
	}

	t, by, err := selectTimeline(ctx, s, flag, lookupEnv)
	failure, ok := errors.AsType[*Error](err)
	if ok && failure.Kind == KindTimelineRequired {
		return report, nil
	}
	if err != nil {
		return report, err
	}

	report.Timeline = &timelineRefJSON{ID: t.ID, Name: t.Name}
	report.SelectedBy = &by
	return report, nil
}

func writeStatus(o output, report statusJSON) error {
	if o.format == formatJSON {
		return o.json(report)
	}

	database, schema, current := report.DBPath, "-", "none"
	if !report.DBExists {
		database += " (does not exist)"
	}
	if report.SchemaVersion != nil {
		schema = strconv.FormatInt(*report.SchemaVersion, 10)
	}
	if report.Timeline != nil {
		current = fmt.Sprintf("%s (selected by %s)", report.Timeline.Name, report.SelectedBy.source())
	}

	var b strings.Builder
	writeField(&b, "Database:", database)
	writeField(&b, "Schema:", schema)
	writeField(&b, "Timeline:", current)
	_, err := io.WriteString(o.out, b.String())
	return err
}

// source names what the user set to make by select the timeline.
func (by selector) source() string {
	switch by {
	case selectedByFlag:
		return "-t"
	case selectedByEnv:
		return "CORONER_TIMELINE"
	case selectedByUse:
		return "'coroner timeline use'"
	}

	return string(by)
}
