package annotation

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationListCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [-e <ref>]",
		Short: "List a timeline's annotations",
		Long: "List a timeline's annotations: those on the whole timeline first, then those on events in\n" +
			"the order the events happened, oldest first. -e lists one event's. Each row is the id,\n" +
			"kind, author, event and the body's first line; piped, the fields are tab-separated.\n\n" +
			"Scripts should treat kind as an open set: later versions may add kinds.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner annotation list\n" +
			"  $ coroner annotation list -e 12 --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listAnnotations(ctx, cmd, app)
		},
	}

	clikit.AddTimelineFlag(cmd.Flags())
	cmd.Flags().StringP("event", "e", "", "Only the annotations on this event, as a `ref`: seq, timeline#seq or id")

	return cmd
}

func listAnnotations(ctx context.Context, cmd *cobra.Command, app clikit.App) error {
	ref, err := clikit.EventFlag(cmd.Flags())
	if err != nil {
		return err
	}
	inv, err := clikit.ReadInvocation(cmd, app)
	if err != nil {
		return err
	}

	return clikit.WithStorePath(ctx, app, inv.Cfg.DBPath, func(s *store.Store) error {
		return s.Read(ctx, func(tx *store.Tx) error {
			t, err := clikit.SelectOne(ctx, app.Logger(), tx, inv.Timelines, app.LookupEnv)
			if err != nil {
				return err
			}
			annotations, events, err := readAnnotations(ctx, tx, t, ref)
			if err != nil {
				return err
			}
			clikit.LogRead(app.Logger(), clikit.CountOf("annotations", len(annotations)), clikit.EventAttrs(ref, events)...)
			order := clikit.OrderEvents(events)
			sortAnnotations(annotations, order)
			if inv.Out.Format == clikit.FormatJSON {
				attachments, err := tx.TimelineAttachments(ctx, t.ID)
				if err != nil {
					return err
				}
				return inv.Out.JSON(newAnnotationListJSON(annotations, clikit.AttachmentsByOwner(attachments, inv.Cfg.DBPath)))
			}

			return printAnnotations(inv.Out, annotations, order)
		})
	})
}

// readAnnotations returns the annotations in t, or on the event ref names when set, with the
// events they may be on in timeline.CompareEvents order.
func readAnnotations(
	ctx context.Context, s *store.Tx, t timeline.Timeline, ref *clikit.EventRef,
) ([]timeline.Annotation, []timeline.Event, error) {
	if ref != nil {
		e, err := clikit.FindEventIn(ctx, s, t, *ref)
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

// sortAnnotations puts the timeline's own annotations first, then the rest in their events'
// order, each group oldest first, then by id.
func sortAnnotations(annotations []timeline.Annotation, order clikit.EventOrder) {
	position := func(a timeline.Annotation) int {
		if a.EventID == nil {
			return -1
		}
		if place, ok := order[*a.EventID]; ok {
			return place.Position
		}
		return math.MaxInt
	}

	slices.SortFunc(annotations, func(a, b timeline.Annotation) int {
		return cmp.Or(cmp.Compare(position(a), position(b)), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
}

func printAnnotations(o clikit.Output, annotations []timeline.Annotation, order clikit.EventOrder) error {
	if len(annotations) == 0 {
		return o.Notify("No annotations yet. Add one with: coroner annotation add <body> --kind comment")
	}

	rows := make([][]string, 0, len(annotations))
	for _, a := range annotations {
		event := "-"
		if a.EventID != nil {
			event = "#" + strconv.FormatInt(order[*a.EventID].Seq, 10)
		}
		firstLine, _, _ := strings.Cut(a.Body, "\n")
		// A CRLF body's first line keeps its \r, which rows would print escaped.
		rows = append(rows, []string{a.ID, string(a.Kind), clikit.OrDash(a.Author), event, strings.TrimSuffix(firstLine, "\r")})
	}

	return o.Rows(rows)
}
