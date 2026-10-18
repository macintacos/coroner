package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventViewCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:         "view <event>",
		Short:       "Show an event and its annotations",
		Long:        "Show every field of an event, then its annotations. --json prints the event object alone.\n\n" + eventRefHelp,
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event view 12\n" +
			"  $ coroner event view mongo-outage-2026-09-11#12 --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewEvent(ctx, cmd, app, args[0])
		},
	}
}

func viewEvent(ctx context.Context, cmd *cobra.Command, app App, ref string) error {
	flag, err := timelineFlag(cmd.Flags())
	if err != nil {
		return err
	}
	parsed, err := parseEventRef(ref)
	if err != nil {
		return err
	}
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	cfg, z, err := resolveZone(cmd, app)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		e, t, err := findEvent(ctx, s, parsed, timelineSelection{flag: flag, lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		if o.format == formatJSON {
			attachments, err := s.ListAttachments(ctx, timeline.AttachmentOwner{EventID: &e.ID})
			if err != nil {
				return err
			}
			return o.json(newEventJSON(e, newAttachmentJSONs(attachments, cfg.DBPath)))
		}

		annotations, err := s.EventAnnotations(ctx, e.ID)
		if err != nil {
			return err
		}

		return writeEventView(o, z, e, t, annotations)
	})
}

// writeEventView writes e's summary, then its annotations, with times in z.
func writeEventView(o output, z zone, e timeline.Event, t timeline.Timeline, annotations []timeline.Annotation) error {
	var b strings.Builder
	b.WriteString(eventSummary(e, t, z))
	b.WriteString("\n")
	writeField(&b, "Annotations:", strconv.Itoa(len(annotations)))

	_, err := io.WriteString(o.out, b.String())
	if err != nil || len(annotations) == 0 {
		return err
	}

	return o.rows(annotationRows(annotations, z))
}

// eventSummary describes e, in t, one field per line under a heading, with times in z.
func eventSummary(e timeline.Event, t timeline.Timeline, z zone) string {
	ended := "-"
	if e.EndedAt != nil {
		ended = e.EndedAt.In(z.loc).Format(time.RFC3339Nano)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Event #%d in timeline `%s`\n", e.Seq, t.Name)
	writeField(&b, "ID:", e.ID)
	writeField(&b, "Occurred:", fmt.Sprintf("%s (%s)", occurredText(e, z), e.Precision))
	writeField(&b, "Ended:", ended)
	writeField(&b, "Typed as:", orDash(e.OccurredRaw))
	writeField(&b, "Source:", orDash(e.Source))
	writeField(&b, "Tags:", cmp.Or(strings.Join(e.Tags, ", "), "-"))
	writeField(&b, "URL:", orDash(e.URL))
	writeField(&b, "Author:", orDash(e.Author))
	writeField(&b, "Created:", displayTime(e.CreatedAt, timeline.PrecisionSecond, z))
	writeField(&b, "Updated:", displayTime(e.UpdatedAt, timeline.PrecisionSecond, z))
	writeField(&b, "Text:", e.Text)

	return b.String()
}

// annotationRows returns a row per annotation: kind, created at in z, author, source and body.
func annotationRows(annotations []timeline.Annotation, z zone) [][]string {
	rows := make([][]string, 0, len(annotations))
	for _, a := range annotations {
		rows = append(rows, []string{
			string(a.Kind), displayTime(a.CreatedAt, timeline.PrecisionSecond, z), orDash(a.Author), orDash(a.Source), a.Body,
		})
	}

	return rows
}
