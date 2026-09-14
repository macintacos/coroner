package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/viewer"
)

func newTimelineViewCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view [<name|id>]",
		Short: "Show a timeline and its events",
		Long: "Show a timeline and its events in the order they happened. Without a name, id or -t,\n" +
			"show the timeline CORONER_TIMELINE or 'coroner timeline use' selects.\n\n" +
			"On a terminal it opens an interactive viewer, where ? lists the keys and q quits.\n" +
			"Expanding an event shows its attachments: images inline in kitty, Ghostty and iTerm2 3.7 or\n" +
			"later, outside tmux and with color on, and elsewhere a placeholder naming each file, which\n" +
			"'coroner attachment export' copies out.\n" +
			"--format text, piping the output, --no-input or CORONER_NO_INPUT prints plain text instead:\n" +
			"a summary, then the events, each with the gap since the event before it. --source, --tag and\n" +
			"--contains keep the events 'coroner event list' keeps with the same flags; --json prints the\n" +
			"timeline object alone.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Annotations:       map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline view mongo-outage-2026-09-11\n" +
			"  $ coroner timeline view --tag election --source mongod\n" +
			"  $ coroner timeline view --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			flag, err := timelineFlag(cmd.Flags())
			switch {
			case err != nil:
				return err
			case len(args) == 1 && flag != nil:
				return &Error{
					Kind:    KindUsage,
					Message: "pass either a timeline name or id, or -t",
					Hint:    "Run 'coroner timeline view --help' for usage.",
				}
			case len(args) == 1:
				return viewTimeline(ctx, cmd, app, func(ctx context.Context, s *store.Store) (timeline.Timeline, error) {
					return findTimeline(ctx, s, args[0])
				})
			}

			return viewTimeline(ctx, cmd, app, func(ctx context.Context, s *store.Store) (timeline.Timeline, error) {
				return selectTimeline(ctx, s, flag, app.LookupEnv)
			})
		},
	}

	addTimelineFlag(cmd.Flags())
	addEventMatchFlags(cmd.Flags())

	return cmd
}

func viewTimeline(
	ctx context.Context, cmd *cobra.Command, app App, lookup func(context.Context, *store.Store) (timeline.Timeline, error),
) error {
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	filter, err := readEventMatch(cmd.Flags())
	if err != nil {
		return err
	}
	cfg, z, err := resolveZone(cmd, app)
	if err != nil {
		return err
	}

	var snapshot viewer.Snapshot
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		t, err := lookup(ctx, s)
		switch {
		case err != nil:
			return err
		case o.format == formatJSON:
			return o.json(newTimelineJSON(t))
		}

		snapshot, err = readSnapshot(ctx, s, t, filter)
		return err
	})
	switch {
	case err != nil || o.format == formatJSON:
		return err
	case !interactive(cmd, app):
		return writeTimelineView(o, z, snapshot.Timeline, snapshot.Events, keptAnnotations(snapshot))
	}

	noColor, _ := cmd.Flags().GetBool("no-color")
	logger, flush := viewerDiagnostics(app)
	term := viewer.Terminal{
		In: app.In, Out: app.Out, NoColor: !colorEnabled(app.OutIsTTY, noColor, app.LookupEnv),
		Graphics: graphics.Detect(app.LookupEnv),
	}
	err = viewer.Run(ctx, term, viewer.Options{
		Snapshot: snapshot,
		Match:    viewer.Match{Sources: filter.Sources, Tags: filter.Tags, Contains: filter.Contains},
		Load:     timelineLoader(ctx, app, cfg.DBPath, snapshot.Timeline.ID),
		Blob:     blobReader(ctx, app, cfg.DBPath, logger),
		Location: z.loc, ZoneLabel: z.label, Now: app.Now,
	})
	err = errors.Join(err, flush())
	if errors.Is(err, viewer.ErrInterrupted) {
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: err}
	}

	return err
}

// interactive is interactiveTerminal, unless --format asked for plain output (I-1).
func interactive(cmd *cobra.Command, app App) bool {
	return interactiveTerminal(app, noInput(cmd, app)) && !cmd.Flags().Changed("format")
}

// timelineLoader returns the viewer's Load for the timeline with timelineID in the database at
// path. Each call checks the match as event list's flags are checked, then opens the database
// read-only, reads and closes it again; its errors carry their hints in their text.
func timelineLoader(ctx context.Context, app App, path, timelineID string) func(viewer.Match) (viewer.Snapshot, error) {
	return func(match viewer.Match) (viewer.Snapshot, error) {
		filter := store.EventFilter{
			Sources: appendUnique(nil, match.Sources...), Tags: appendUnique(nil, match.Tags...), Contains: match.Contains,
		}
		err := checkFilterSlugs(filter)
		if err != nil {
			return viewer.Snapshot{}, hinted(err)
		}

		var snapshot viewer.Snapshot
		err = withStoreOpenedBy(ctx, app.OpenStoreReadOnly, path, func(s *store.Store) error {
			t, err := s.Timeline(ctx, timelineID)
			if err != nil {
				return err
			}

			snapshot, err = readSnapshot(ctx, s, t, filter)
			return err
		})

		return snapshot, hinted(err)
	}
}

// viewerDiagnostics returns a logger for debug detail found while the viewer holds the screen,
// and a flush that writes what it logged to app.Err once the viewer has closed. Without --debug
// or CORONER_DEBUG it logs nothing.
func viewerDiagnostics(app App) (*slog.Logger, func() error) {
	_, debug := errorStyleFor(app)
	if !debug {
		return slog.New(slog.DiscardHandler), func() error { return nil }
	}

	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))

	return logger, func() error {
		_, err := app.Err.Write(logged.Bytes())
		return err
	}
}

// blobReader returns the viewer's Blob for the database at path: it reads an attachment's blob,
// logging why it could not.
func blobReader(ctx context.Context, app App, path string, logger *slog.Logger) func(timeline.Attachment) ([]byte, error) {
	return func(a timeline.Attachment) ([]byte, error) {
		content, err := app.ReadFile(store.BlobPath(path, a.SHA256))
		if err != nil {
			logger.DebugContext(ctx, "attachment blob unreadable", "attachment", a.ID, "sha256", a.SHA256, "error", err)
		}

		return content, err
	}
}

// hintedError shows an *Error's message and hint as its text, for a screen that shows only
// Error().
type hintedError struct {
	err *Error
}

func (e hintedError) Error() string { return e.err.Message + ". " + e.err.Hint }

func (e hintedError) Unwrap() error { return e.err }

// hinted returns err with its *Error's hint in its text, when it has one.
func hinted(err error) error {
	typed, ok := errors.AsType[*Error](err)
	if !ok || typed.Hint == "" {
		return err
	}

	return hintedError{err: typed}
}

// readSnapshot reads t's events that filter keeps, and every annotation and attachment in t.
func readSnapshot(ctx context.Context, s *store.Store, t timeline.Timeline, filter store.EventFilter) (viewer.Snapshot, error) {
	page, err := s.ListEvents(ctx, t.ID, filter)
	if err != nil {
		return viewer.Snapshot{}, err
	}
	annotations, err := s.Annotations(ctx, t.ID)
	if err != nil {
		return viewer.Snapshot{}, err
	}
	attachments, err := s.TimelineAttachments(ctx, t.ID)

	return viewer.Snapshot{Timeline: t, Events: page.Events, Annotations: annotations, Attachments: attachments}, err
}

// keptAnnotations counts snapshot's annotations on its events or on the timeline itself.
func keptAnnotations(snapshot viewer.Snapshot) int {
	kept := make(map[string]bool, len(snapshot.Events))
	for _, e := range snapshot.Events {
		kept[e.ID] = true
	}

	count := 0
	for _, a := range snapshot.Annotations {
		if a.EventID == nil || kept[*a.EventID] {
			count++
		}
	}

	return count
}

// writeTimelineView writes t's summary, including annotationCount, then events, which must
// already be in timeline.CompareEvents order, with times in z.
func writeTimelineView(o output, z zone, t timeline.Timeline, events []timeline.Event, annotationCount int) error {
	sources, tags := map[string]int{}, map[string]int{}
	for _, e := range events {
		if e.Source != nil {
			sources[*e.Source]++
		}
		for _, tag := range e.Tags {
			tags[tag]++
		}
	}

	var b strings.Builder
	b.WriteString(timelineSummary(t, z))
	writeField(&b, "Events:", eventSpan(events, z))
	writeField(&b, "Sources:", countSummary(sources))
	writeField(&b, "Tags:", countSummary(tags))
	writeField(&b, "Annotations:", strconv.Itoa(annotationCount))
	if len(events) > 0 {
		b.WriteString("\n")
	}

	_, err := io.WriteString(o.out, b.String())
	if err != nil {
		return err
	}

	return o.rows(eventRows(events, z))
}

// timelineSummary describes t itself, one field per line under its name, with times in z.
func timelineSummary(t timeline.Timeline, z zone) string {
	var b strings.Builder
	b.WriteString(t.Name + "\n")
	writeField(&b, "ID:", t.ID)
	writeField(&b, "Title:", cmp.Or(t.Title, "-"))
	writeField(&b, "Refs:", cmp.Or(strings.Join(t.Refs, ", "), "-"))
	writeField(&b, "Created:", displayTime(t.CreatedAt, timeline.PrecisionSecond, z))

	return b.String()
}

func writeField(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%-13s%s\n", label, value)
}

func eventSpan(events []timeline.Event, z zone) string {
	if len(events) == 0 {
		return "0"
	}

	return fmt.Sprintf("%d, from %s to %s", len(events),
		occurredText(events[0], z), occurredText(events[len(events)-1], z))
}

// countSummary renders counts as "name count" pairs in name order.
func countSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}

	pairs := make([]string, 0, len(counts))
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		pairs = append(pairs, fmt.Sprintf("%s %d", name, counts[name]))
	}

	return strings.Join(pairs, ", ")
}

// eventRows returns a row per event: seq, occurred at in z, the gap since the previous
// event, source and text.
func eventRows(events []timeline.Event, z zone) [][]string {
	rows := make([][]string, 0, len(events))
	for i, e := range events {
		gap := ""
		if i > 0 {
			gap = "+" + e.OccurredAt.Sub(events[i-1].OccurredAt).String()
		}
		source := "-"
		if e.Source != nil {
			source = *e.Source
		}

		rows = append(rows, []string{"#" + strconv.FormatInt(e.Seq, 10), occurredText(e, z), gap, source, e.Text})
	}

	return rows
}
