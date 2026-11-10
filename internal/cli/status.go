package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/store"
)

func newStatusCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the database and the timeline commands act on",
		Long: "Show the database coroner uses, its schema version, the timeline commands act on, and\n" +
			"whether -t, CORONER_TIMELINE or 'coroner timeline use' selected it. status never\n" +
			"migrates the database or creates it. Its JSON fields are experimental until 1.0.",
		GroupID:     clikit.AdditionalCommands,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner status\n" +
			"  $ CORONER_TIMELINE=mongo-outage-2026-09-11 coroner status --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return showStatus(ctx, cmd, app)
		},
	}

	clikit.AddTimelineFlag(cmd.Flags())

	return cmd
}

// statusJSON is status's JSON object. A nil pointer encodes as null: no database, or no
// timeline selected.
type statusJSON struct {
	DBPath        string                `json:"db_path"`
	DBExists      bool                  `json:"db_exists"`
	SchemaVersion *int64                `json:"schema_version"`
	Timeline      *selectedTimelineJSON `json:"timeline"`
	SelectedBy    *clikit.SelectedBy    `json:"selected_by"`
}

// selectedTimelineJSON names the timeline status reports as selected.
type selectedTimelineJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func showStatus(ctx context.Context, cmd *cobra.Command, app clikit.App) error {
	inv, err := clikit.ReadInvocation(cmd, app)
	if err != nil {
		return err
	}

	var report statusJSON
	err = clikit.WithStoreOpenedBy(ctx, app.OpenStoreReadOnly, inv.Cfg.DBPath, func(s *store.Store) error {
		var err error
		report, err = readStatus(ctx, app.Logger(), s, inv.Timelines, app.LookupEnv)
		if err != nil {
			return err
		}
		timelinesRead := 0
		if report.Timeline != nil {
			timelinesRead = 1
		}
		clikit.LogRead(app.Logger(), clikit.CountOf("timelines", timelinesRead))
		return nil
	})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return writeStatus(inv.Out, statusJSON{DBPath: inv.Cfg.DBPath})
	case err != nil:
		return err
	}

	report.DBPath = inv.Cfg.DBPath
	return writeStatus(inv.Out, report)
}

// readStatus reads the schema version of the database s, and the timeline -t,
// CORONER_TIMELINE or the current timeline selects.
func readStatus(
	ctx context.Context, logger *slog.Logger, s *store.Store, timelines clikit.TimelineInput, lookupEnv func(string) (string, bool),
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

	sel, err := clikit.SelectTimelines(ctx, logger, s, timelines, lookupEnv)
	if err != nil || sel.By == "" {
		return report, err
	}

	t := sel.Timelines[0]
	report.Timeline = &selectedTimelineJSON{ID: t.ID, Name: t.Name}
	report.SelectedBy = &sel.By
	return report, nil
}

func writeStatus(o clikit.Output, report statusJSON) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(report)
	}

	database, schema, timelineText := report.DBPath, "-", "none"
	if !report.DBExists {
		database += " (does not exist)"
	}
	if report.SchemaVersion != nil {
		schema = strconv.FormatInt(*report.SchemaVersion, 10)
	}
	if report.Timeline != nil {
		timelineText = fmt.Sprintf("%s (selected by %s)", report.Timeline.Name, report.SelectedBy.Setting())
	}

	var b strings.Builder
	clikit.WriteField(&b, "Database:", database)
	clikit.WriteField(&b, "Schema:", schema)
	clikit.WriteField(&b, "Timeline:", timelineText)
	_, err := io.WriteString(o.Out, b.String())
	return err
}
