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
		Short: "Show the database and the timeline commands act on",
		Long: "Show the database coroner uses, its schema version, the timeline commands act on, and\n" +
			"whether -t, CORONER_TIMELINE or 'coroner timeline use' selected it. status never\n" +
			"migrates the database or creates it. Its JSON fields are experimental until 1.0.",
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

// statusJSON is status's JSON object. A nil pointer encodes as null: no database, or no
// timeline selected.
type statusJSON struct {
	DBPath        string                `json:"db_path"`
	DBExists      bool                  `json:"db_exists"`
	SchemaVersion *int64                `json:"schema_version"`
	Timeline      *selectedTimelineJSON `json:"timeline"`
	SelectedBy    *selectedBy           `json:"selected_by"`
}

// selectedTimelineJSON names the timeline status reports as selected.
type selectedTimelineJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func showStatus(ctx context.Context, cmd *cobra.Command, app App) error {
	inv, err := readInvocation(cmd, app)
	if err != nil {
		return err
	}

	var report statusJSON
	err = withStoreOpenedBy(ctx, app.OpenStoreReadOnly, inv.cfg.DBPath, func(s *store.Store) error {
		var err error
		report, err = readStatus(ctx, s, inv.timelines, app.LookupEnv)
		return err
	})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return writeStatus(inv.out, statusJSON{DBPath: inv.cfg.DBPath})
	case err != nil:
		return err
	}

	report.DBPath = inv.cfg.DBPath
	return writeStatus(inv.out, report)
}

// readStatus reads the schema version of the database s, and the timeline -t,
// CORONER_TIMELINE or the current timeline selects.
func readStatus(
	ctx context.Context, s *store.Store, timelines timelineInput, lookupEnv func(string) (string, bool),
) (statusJSON, error) {
	schema, err := s.Schema(ctx)
	if err != nil {
		return statusJSON{}, err
	}
	report := statusJSON{DBExists: true, SchemaVersion: &schema.Version}
	// Selection reads expect the latest schema, which a read-only open never migrates to.
	if schema.Version < schema.Latest {
		return report, nil
	}

	sel, err := selectTimelines(ctx, s, timelines, lookupEnv)
	if err != nil || sel.by == "" {
		return report, err
	}

	t := sel.timelines[0]
	report.Timeline = &selectedTimelineJSON{ID: t.ID, Name: t.Name}
	report.SelectedBy = &sel.by
	return report, nil
}

func writeStatus(o output, report statusJSON) error {
	if o.format == formatJSON {
		return o.json(report)
	}

	database, schema, timelineText := report.DBPath, "-", "none"
	if !report.DBExists {
		database += " (does not exist)"
	}
	if report.SchemaVersion != nil {
		schema = strconv.FormatInt(*report.SchemaVersion, 10)
	}
	if report.Timeline != nil {
		timelineText = fmt.Sprintf("%s (selected by %s)", report.Timeline.Name, report.SelectedBy.setting())
	}

	var b strings.Builder
	writeField(&b, "Database:", database)
	writeField(&b, "Schema:", schema)
	writeField(&b, "Timeline:", timelineText)
	_, err := io.WriteString(o.out, b.String())
	return err
}
