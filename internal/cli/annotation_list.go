package cli

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationListCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [-e <ref>]",
		Short: "List a timeline's annotations",
		Long: "List a timeline's annotations: those on the whole timeline first, then those on events in\n" +
			"the order the events happened, oldest first. -e lists one event's. Each row is the id,\n" +
			"kind, author, event and the body's first line; piped, the fields are tab-separated.\n\n" +
			"Scripts should treat kind as an open set: later versions may add kinds.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner annotation list\n" +
			"  $ coroner annotation list -e 12 --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listAnnotations(ctx, cmd, app)
		},
	}

	addTimelineFlag(cmd.Flags())
	cmd.Flags().StringP("event", "e", "", "Only the annotations on this event, as a `ref`: seq, timeline#seq or id")

	return cmd
}

func listAnnotations(ctx context.Context, cmd *cobra.Command, app App) error {
	flag, err := timelineFlag(cmd.Flags())
	if err != nil {
		return err
	}
	ref, err := eventFlag(cmd.Flags())
	if err != nil {
		return err
	}
	inv, err := readInvocation(cmd, app)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, inv.cfg.DBPath, func(s *store.Store) error {
		t, err := selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		annotations, events, err := readAnnotations(ctx, s, t, ref, timelineSelection{flag: flag, lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		order := orderEvents(events)
		sortAnnotations(annotations, order)
		if inv.out.format == formatJSON {
			attachments, err := s.TimelineAttachments(ctx, t.ID)
			if err != nil {
				return err
			}
			return inv.out.json(newAnnotationListJSON(annotations, attachmentsByOwner(attachments, inv.cfg.DBPath)))
		}

		return printAnnotations(inv.out, annotations, order)
	})
}

// readAnnotations returns the annotations in t, or on the event ref names when set, with the
// events they may be on in timeline.CompareEvents order.
// ponytail: annotations and events are read in separate transactions, so an event deleted in
// between lists its annotations last, and text shows them on event #0; read both in one store
// call if that matters.
func readAnnotations(
	ctx context.Context, s *store.Store, t timeline.Timeline, ref *eventRef, selection timelineSelection,
) ([]timeline.Annotation, []timeline.Event, error) {
	if ref != nil {
		e, err := findEventIn(ctx, s, t, *ref, selection)
		if err != nil {
			return nil, nil, err
		}
		annotations, err := s.EventAnnotations(ctx, e.ID)
		return annotations, []timeline.Event{e}, err
	}

	annotations, err := s.Annotations(ctx, t.ID)
	if err != nil || len(annotations) == 0 {
		return annotations, nil, err
	}
	events, err := s.Events(ctx, t.ID)

	return annotations, events, err
}

// eventPlace is where an event sits in its timeline: its position in time order, and its seq.
type eventPlace struct {
	position int
	seq      int64
}

// eventOrder is each event's place, keyed by event id.
type eventOrder map[string]eventPlace

// orderEvents indexes events, which must be in timeline.CompareEvents order.
func orderEvents(events []timeline.Event) eventOrder {
	order := make(eventOrder, len(events))
	for i, e := range events {
		order[e.ID] = eventPlace{position: i, seq: e.Seq}
	}

	return order
}

// sortAnnotations puts the timeline's own annotations first, then the rest in their events'
// order, each group oldest first, then by id.
func sortAnnotations(annotations []timeline.Annotation, order eventOrder) {
	position := func(a timeline.Annotation) int {
		if a.EventID == nil {
			return -1
		}
		if place, ok := order[*a.EventID]; ok {
			return place.position
		}
		return math.MaxInt
	}

	slices.SortFunc(annotations, func(a, b timeline.Annotation) int {
		return cmp.Or(cmp.Compare(position(a), position(b)), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
}

func printAnnotations(o output, annotations []timeline.Annotation, order eventOrder) error {
	if len(annotations) == 0 {
		return o.notify("No annotations yet. Add one with: coroner annotation add <body> --kind comment")
	}

	rows := make([][]string, 0, len(annotations))
	for _, a := range annotations {
		event := "-"
		if a.EventID != nil {
			event = "#" + strconv.FormatInt(order[*a.EventID].seq, 10)
		}
		firstLine, _, _ := strings.Cut(a.Body, "\n")
		// A CRLF body's first line keeps its \r, which rows would print escaped.
		rows = append(rows, []string{a.ID, string(a.Kind), orDash(a.Author), event, strings.TrimSuffix(firstLine, "\r")})
	}

	return o.rows(rows)
}
