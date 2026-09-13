package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineViewCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view [<name|id>]",
		Short: "Show a timeline and its events",
		Long: "Show a timeline: a summary, then its events in the order they happened, each with the\n" +
			"time since the event before it. Without a name, id or -t, show the timeline\n" +
			"CORONER_TIMELINE or 'coroner timeline use' selects.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Annotations:       map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline view mongo-outage-2026-09-11\n" +
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
				t, _, err := selectTimeline(ctx, s, flag, app.LookupEnv)
				return t, err
			})
		},
	}

	addTimelineFlag(cmd.Flags())

	return cmd
}

func viewTimeline(
	ctx context.Context, cmd *cobra.Command, app App, lookup func(context.Context, *store.Store) (timeline.Timeline, error),
) error {
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}

	return withStore(ctx, cmd, app, func(s *store.Store) error {
		t, err := lookup(ctx, s)
		if err != nil {
			return err
		}
		if o.format == formatJSON {
			return o.json(newTimelineJSON(t))
		}

		events, err := s.Events(ctx, t.ID)
		if err != nil {
			return err
		}
		annotations, err := s.Annotations(ctx, t.ID)
		if err != nil {
			return err
		}

		return writeTimelineView(o, t, events, len(annotations))
	})
}

// writeTimelineView writes t's summary, including annotationCount, then events, which must
// already be in timeline.CompareEvents order.
func writeTimelineView(o output, t timeline.Timeline, events []timeline.Event, annotationCount int) error {
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
	b.WriteString(timelineSummary(t))
	writeField(&b, "Events:", eventSpan(events))
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

	return o.rows(eventRows(events))
}

// timelineSummary describes t itself, one field per line under its name.
func timelineSummary(t timeline.Timeline) string {
	var b strings.Builder
	b.WriteString(t.Name + "\n")
	writeField(&b, "ID:", t.ID)
	writeField(&b, "Title:", cmp.Or(t.Title, "-"))
	writeField(&b, "Refs:", cmp.Or(strings.Join(t.Refs, ", "), "-"))
	writeField(&b, "Created:", displayInstant(t.CreatedAt))

	return b.String()
}

func writeField(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "%-13s%s\n", label, value)
}

func eventSpan(events []timeline.Event) string {
	if len(events) == 0 {
		return "0"
	}

	return fmt.Sprintf("%d, from %s to %s", len(events),
		displayInstant(events[0].OccurredAt), displayInstant(events[len(events)-1].OccurredAt))
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

// eventRows returns a row per event: seq, occurred at, the gap since the previous event,
// source and text.
func eventRows(events []timeline.Event) [][]string {
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

		rows = append(rows, []string{"#" + strconv.FormatInt(e.Seq, 10), displayInstant(e.OccurredAt), gap, source, e.Text})
	}

	return rows
}
