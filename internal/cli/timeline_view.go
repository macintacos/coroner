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
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newTimelineViewCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:   "view [<name|id>]",
		Short: "Show a timeline and its events",
		Long: "Show a timeline: a summary, then its events in the order they happened, each with the\n" +
			"time since the event before it. Without a name or id, show the current timeline.",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner timeline view mongo-outage-2026-09-11\n" +
			"  $ coroner timeline view --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(ctx, cmd, app, func(s *store.Store) error {
				t, err := viewedTimeline(ctx, s, args)
				if err != nil {
					return err
				}
				if printsJSON(cmd) {
					return writeJSON(app.Out, app.OutIsTTY, newTimelineJSON(t))
				}

				events, err := s.Events(ctx, t.ID)
				if err != nil {
					return err
				}
				annotations, err := s.Annotations(ctx, t.ID)
				if err != nil {
					return err
				}

				return writeTimelineView(app.Out, t, events, len(annotations))
			})
		},
	}
}

// viewedTimeline returns the timeline args names, or the current timeline when args is empty.
func viewedTimeline(ctx context.Context, s *store.Store, args []string) (timeline.Timeline, error) {
	if len(args) == 1 {
		return findTimeline(ctx, s, args[0])
	}

	t, err := s.CurrentTimeline(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return t, &Error{
			Kind:    KindTimelineRequired,
			Message: "no timeline was named and none is current",
			Hint:    "Pass a timeline name, or run 'coroner timeline use <name>'.",
			Err:     err,
		}
	}

	return t, err
}

// writeTimelineView writes t's summary, with counts over events and its annotations, then its
// events, which must be in timeline.CompareEvents order.
func writeTimelineView(w io.Writer, t timeline.Timeline, events []timeline.Event, annotations int) error {
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
	writeField(&b, "Annotations:", strconv.Itoa(annotations))

	_, err := io.WriteString(w, b.String())
	if err != nil || len(events) == 0 {
		return err
	}

	return writeEvents(w, events)
}

// timelineSummary describes t itself, one field per line under its name.
func timelineSummary(t timeline.Timeline) string {
	var b strings.Builder
	b.WriteString(t.Name + "\n")
	writeField(&b, "ID:", t.ID)
	writeField(&b, "Title:", cmp.Or(t.Title, "-"))
	writeField(&b, "Refs:", cmp.Or(strings.Join(t.Refs, ", "), "-"))
	writeField(&b, "Created:", timeline.FormatInstant(t.CreatedAt))

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
		timeline.FormatInstant(events[0].OccurredAt), timeline.FormatInstant(events[len(events)-1].OccurredAt))
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

// writeEvents writes a blank line, then one aligned row per event: seq, occurred at, the gap
// since the previous event, source and text.
func writeEvents(w io.Writer, events []timeline.Event) error {
	var rows strings.Builder
	rows.WriteString("\n")
	for i, e := range events {
		gap := ""
		if i > 0 {
			gap = "+" + e.OccurredAt.Sub(events[i-1].OccurredAt).String()
		}
		source := "-"
		if e.Source != nil {
			source = *e.Source
		}

		// ponytail: instants print in UTC; show them in the --tz zone once EXC-1308 resolves it.
		fmt.Fprintf(&rows, "#%d\t%s\t%s\t%s\t%s\n", e.Seq, timeline.FormatInstant(e.OccurredAt), gap, source, e.Text)
	}

	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, err := io.WriteString(table, rows.String())
	if err != nil {
		return err
	}

	return table.Flush()
}
