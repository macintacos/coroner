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
	"sync"

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
			"later, outside a multiplexer such as tmux and with color on, and elsewhere a placeholder\n" +
			"naming each file, which 'coroner attachment export' copies out.\n" +
			"--format text, piping the output, --no-input or CORONER_NO_INPUT prints plain text instead:\n" +
			"a summary, then the events, each with the gap since the event before it. --source, --tag and\n" +
			"--contains keep the events 'coroner event list' keeps with the same flags; --json prints the\n" +
			"timeline object alone.\n\n" +
			"Repeat -t to view several timelines as one chronology: their events merge by occurred at, then\n" +
			"timeline id, then seq. The viewer names each event's timeline in a column tinted with that\n" +
			"timeline's color, and plain text prints each timeline's summary and starts each event's row\n" +
			"with its timeline's name. --json prints one timeline object, so it takes one timeline.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTimelineArg(ctx, app),
		Annotations: map[string]string{
			formatsAnnotation: "text,json", timelinesAnnotation: "several,positional", oneTimelineFormatsAnnotation: "json",
		},
		Example: "  $ coroner timeline view mongo-outage-2026-09-11\n" +
			"  $ coroner timeline view --tag election --source mongod\n" +
			"  $ coroner timeline view -t mongo-failover -t checkout-latency\n" +
			"  $ coroner timeline view --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			names, err := timelineFlags(cmd.Flags())
			switch {
			case err != nil:
				return err
			case len(args) == 1 && len(names) > 0:
				return &Error{
					Kind:    KindUsage,
					Message: "pass either a timeline name or id, or -t",
					Hint:    "Run 'coroner timeline view --help' for usage.",
				}
			case len(args) == 1:
				return viewTimelines(ctx, cmd, app, func(ctx context.Context, s *store.Store) ([]timeline.Timeline, error) {
					t, err := findTimeline(ctx, s, args[0])
					return []timeline.Timeline{t}, err
				})
			}

			return viewTimelines(ctx, cmd, app, func(ctx context.Context, s *store.Store) ([]timeline.Timeline, error) {
				return selectTimelines(ctx, s, names, app.LookupEnv)
			})
		},
	}

	addTimelineFlag(cmd.Flags())
	cmd.Flags().Lookup("timeline").Usage = "Timeline `name|id` to view; repeat to view several as one chronology"
	addEventMatchFlags(cmd.Flags())

	return cmd
}

func viewTimelines(
	ctx context.Context, cmd *cobra.Command, app App, lookup func(context.Context, *store.Store) ([]timeline.Timeline, error),
) error {
	filter, err := readEventMatch(cmd.Flags())
	if err != nil {
		return err
	}
	inv, err := readInvocation(cmd, app)
	if err != nil {
		return err
	}
	z, err := inv.zone()
	if err != nil {
		return err
	}
	o, cfg := inv.out, inv.cfg

	var snapshot viewer.Snapshot
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		timelines, err := lookup(ctx, s)
		switch {
		case err != nil:
			return err
		case o.format == formatJSON && len(timelines) > 1:
			return &Error{
				Kind:    KindUsage,
				Message: fmt.Sprintf("--format json prints one timeline, and -t selects %d", len(timelines)),
				Hint:    "Pass -t once, or list the timelines' events with 'coroner event list -t <timeline>... --json'.",
			}
		case o.format == formatJSON:
			return o.json(newTimelineJSON(timelines[0]))
		}

		snapshot, err = readSnapshot(ctx, s, timelines, filter)
		return err
	})
	switch {
	case err != nil || o.format == formatJSON:
		return err
	case !inv.interactive():
		return writeTimelineView(o, z, snapshot)
	}

	logger, flush := viewerDiagnostics(inv.debug, app.Err)
	color := inv.outColor
	images := graphics.None
	if color {
		images = graphics.Detect(app.LookupEnv)
	}
	term := viewer.Terminal{In: app.In, Out: app.Out, NoColor: !color, Graphics: images}
	err = viewer.Run(ctx, term, viewer.Options{
		Snapshot: snapshot,
		Match:    viewer.Match{Sources: filter.Sources, Tags: filter.Tags, Contains: filter.Contains},
		Load:     timelineLoader(ctx, app, cfg.DBPath, timelineIDs(snapshot.Timelines)),
		Image:    imageReader(app.ReadFile, cfg.DBPath, images, logger),
		Location: z.loc, ZoneLabel: z.label, Now: app.Now,
	})
	err = errors.Join(err, flush())
	if errors.Is(err, viewer.ErrInterrupted) {
		return &Error{Kind: KindInterrupted, Message: "interrupted", Err: err}
	}

	return err
}

// timelineLoader returns the viewer's Load for the timelines with timelineIDs in the database at
// path. Each call checks the match as event list's flags are checked, then opens the database
// read-only, reads and closes it again; its errors carry their hints in their text.
func timelineLoader(ctx context.Context, app App, path string, timelineIDs []string) func(viewer.Match) (viewer.Snapshot, error) {
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
			timelines, err := timelinesByID(ctx, s, timelineIDs)
			if err != nil {
				return err
			}

			snapshot, err = readSnapshot(ctx, s, timelines, filter)
			return err
		})

		return snapshot, hinted(err)
	}
}

// viewerDiagnostics returns a logger for debug detail found while the viewer holds the screen,
// and a flush that writes what it logged to errOut once the viewer has closed. Without debug it
// logs nothing.
func viewerDiagnostics(debug bool, errOut io.Writer) (*slog.Logger, func() error) {
	if !debug {
		return slog.New(slog.DiscardHandler), func() error { return nil }
	}

	logged := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))

	return logger, func() error {
		_, err := logged.WriteTo(errOut)
		return err
	}
}

// lockedBuffer is a buffer a flush can drain while commands still write to it: bubbletea does not
// wait for their goroutines when the viewer closes.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.Write(p)
}

func (l *lockedBuffer) WriteTo(w io.Writer) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.WriteTo(w)
}

// imageReader returns the viewer's Image for the database at path: it reads an attachment's blob
// and decodes it when protocol shows images, or reads only its size otherwise, logging why it
// could not.
func imageReader(
	readFile func(string) ([]byte, error), path string, protocol graphics.Protocol, logger *slog.Logger,
) func(timeline.Attachment) (graphics.Image, error) {
	decode := graphics.DecodeSize
	if protocol == graphics.Kitty {
		decode = graphics.Decode
	}

	return func(a timeline.Attachment) (graphics.Image, error) {
		content, err := readFile(store.BlobPath(path, a.SHA256))
		img := graphics.Image{}
		if err == nil {
			img, err = decode(content)
		}
		if err != nil {
			logger.Debug("attachment image not shown", "attachment", a.ID, "sha256", a.SHA256, "error", err)
		}

		return img, err
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

// readSnapshot reads the events of timelines that filter keeps, merged in one order, and every
// annotation and attachment in them.
func readSnapshot(ctx context.Context, s *store.Store, timelines []timeline.Timeline, filter store.EventFilter) (viewer.Snapshot, error) {
	filter.TimelineIDs = timelineIDs(timelines)
	page, err := s.ListEvents(ctx, filter)
	if err != nil {
		return viewer.Snapshot{}, err
	}
	annotations, err := readEach(timelines, func(id string) ([]timeline.Annotation, error) { return s.Annotations(ctx, id) })
	if err != nil {
		return viewer.Snapshot{}, err
	}
	attachments, err := readEach(timelines, func(id string) ([]timeline.Attachment, error) { return s.TimelineAttachments(ctx, id) })

	return viewer.Snapshot{Timelines: timelines, Events: page.Events, Annotations: annotations, Attachments: attachments}, err
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

// writeTimelineView writes the summary of each of snapshot's timelines, then of its events and
// the annotations on them or on a whole timeline, then the events, with times in z. In an overlay
// of several timelines, each event's row starts with its timeline's name, as event list's do.
func writeTimelineView(o output, z zone, snapshot viewer.Snapshot) error {
	events := snapshot.Events
	sources, tags := map[string]int{}, map[string]int{}
	for _, e := range events {
		if e.Source != nil {
			sources[*e.Source]++
		}
		for _, tag := range e.Tags {
			tags[tag]++
		}
	}

	overlay := len(snapshot.Timelines) > 1
	var b strings.Builder
	for _, t := range snapshot.Timelines {
		b.WriteString(timelineSummary(t, z))
		if overlay {
			b.WriteString("\n")
		}
	}
	writeField(&b, "Events:", eventSpan(events, z))
	writeField(&b, "Sources:", countSummary(sources))
	writeField(&b, "Tags:", countSummary(tags))
	writeField(&b, "Annotations:", strconv.Itoa(keptAnnotations(snapshot)))
	if len(events) > 0 {
		b.WriteString("\n")
	}

	_, err := io.WriteString(o.out, b.String())
	if err != nil {
		return err
	}
	rows := eventRows(events, z)
	if overlay {
		rows = withTimelineNames(rows, events, snapshot.Timelines)
	}

	return o.rows(rows)
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
