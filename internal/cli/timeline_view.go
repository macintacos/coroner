package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/viewer"
)

func newTimelineViewCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view [<name|id>]",
		Short: "Show a timeline and its events",
		Long: "Show a timeline: a summary, then its events in the order they happened, each with the\n" +
			"time since the event before it. Without a name, id or -t, show the timeline\n" +
			"CORONER_TIMELINE or 'coroner timeline use' selects.\n\n" +
			"On a terminal it opens an interactive viewer, where ? lists the keys and q quits.\n" +
			"--format text, or piping the output, prints plain text instead. --source, --tag and\n" +
			"--contains keep the events 'coroner event list' keeps with the same flags; --json prints\n" +
			"the timeline object alone.",
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
		if err != nil || o.format == formatJSON {
			snapshot.Timeline = t
			return err
		}

		snapshot, err = readSnapshot(ctx, s, t, filter)
		return err
	})
	switch {
	case err != nil:
		return err
	case o.format == formatJSON:
		return o.json(newTimelineJSON(snapshot.Timeline))
	case !app.InIsTTY || !app.OutIsTTY || cmd.Flags().Changed("format"):
		return writeTimelineView(o, z, snapshot.Timeline, snapshot.Events, len(snapshot.Annotations))
	}

	noColor, _ := cmd.Flags().GetBool("no-color")
	err = viewer.Run(ctx, viewer.Terminal{In: app.In, Out: app.Out, NoColor: !colorEnabled(app.OutIsTTY, noColor, app.LookupEnv)},
		viewer.Options{
			Snapshot: snapshot, Filter: filter, Load: timelineLoader(ctx, app, cfg.DBPath, snapshot.Timeline.ID),
			Location: z.loc, Now: app.Now,
		})
	if errors.Is(err, viewer.ErrInterrupted) {
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: err}
	}

	return err
}

// timelineLoader returns the viewer's Load for the timeline with timelineID in the database at
// path, which opens the database, reads and closes it again on every call.
func timelineLoader(ctx context.Context, app App, path, timelineID string) func(store.EventFilter) (viewer.Snapshot, error) {
	return func(filter store.EventFilter) (viewer.Snapshot, error) {
		var snapshot viewer.Snapshot
		err := withStorePath(ctx, app, path, func(s *store.Store) error {
			t, err := s.Timeline(ctx, timelineID)
			if err != nil {
				return err
			}

			snapshot, err = readSnapshot(ctx, s, t, filter)
			return err
		})

		return snapshot, err
	}
}

// readSnapshot reads t's events that filter keeps and every annotation in t.
func readSnapshot(ctx context.Context, s *store.Store, t timeline.Timeline, filter store.EventFilter) (viewer.Snapshot, error) {
	page, err := s.ListEvents(ctx, t.ID, filter)
	if err != nil {
		return viewer.Snapshot{}, err
	}
	annotations, err := s.Annotations(ctx, t.ID)

	return viewer.Snapshot{Timeline: t, Events: page.Events, Annotations: annotations}, err
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
