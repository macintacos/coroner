package event

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventViewCommand(ctx context.Context, app clikit.App) *cobra.Command {
	return &cobra.Command{
		Use:   "view <event>",
		Short: "Show an event, its annotations and its links",
		Long: "Show every field of an event, then its annotations, then its links to and from other events\n" +
			"in any timeline. --json prints the event object with its links.\n\n" + clikit.EventRefHelp,
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner event view 12\n" +
			"  $ coroner event view mongo-outage-2026-09-11#12 --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return viewEvent(ctx, cmd, app, args[0])
		},
	}
}

func viewEvent(ctx context.Context, cmd *cobra.Command, app clikit.App, ref string) error {
	parsed, err := clikit.ParseEventRef(ref)
	if err != nil {
		return err
	}
	inv, z, err := clikit.ReadInvocationInZone(cmd, app)
	if err != nil {
		return err
	}

	return clikit.WithStorePath(ctx, app, inv.Cfg.DBPath, func(s *store.Store) error {
		return s.Read(ctx, func(tx *store.Tx) error {
			e, t, err := clikit.FindEvent(ctx, app.Logger(), tx, parsed, inv.Timelines, app.LookupEnv)
			if err != nil {
				return err
			}
			links, err := tx.EventLinks(ctx, e.ID)
			if err != nil {
				return err
			}
			if inv.Out.Format == clikit.FormatJSON {
				attachments, err := tx.ListAttachments(ctx, timeline.AttachmentOwner{EventID: &e.ID})
				if err != nil {
					return err
				}
				clikit.LogRead(app.Logger(), slog.GroupValue(slog.Int("links", len(links)), slog.Int("attachments", len(attachments))), slog.String("event", e.ID))
				return inv.Out.JSON(eventViewJSON{JSON: NewJSON(e, clikit.NewAttachmentJSONs(attachments, inv.Cfg.DBPath)), Links: newLinkJSONs(links)})
			}

			annotations, err := tx.EventAnnotations(ctx, e.ID)
			if err != nil {
				return err
			}
			linkRows, err := eventLinkRows(ctx, tx, e.ID, links)
			if err != nil {
				return err
			}
			clikit.LogRead(app.Logger(), slog.GroupValue(slog.Int("links", len(links)), slog.Int("annotations", len(annotations))), slog.String("event", e.ID))

			return writeEventView(inv.Out, z, eventView{event: e, timeline: t, annotations: annotations, linkRows: linkRows})
		})
	})
}

// eventViewJSON is event view's object: the event's, with its links in either direction.
type eventViewJSON struct {
	JSON

	// Never nil, so no links encode as [] rather than null.
	Links []export.Link `json:"links"`
}

func newLinkJSONs(links []timeline.Link) []export.Link {
	objects := make([]export.Link, 0, len(links))
	for _, l := range links {
		objects = append(objects, export.NewLink(l))
	}

	return objects
}

// eventView is what event view shows in text.
type eventView struct {
	event       timeline.Event
	timeline    timeline.Timeline
	annotations []timeline.Annotation
	linkRows    [][]string
}

// writeEventView writes the event's summary and annotations, with times in z, then its links.
func writeEventView(o clikit.Output, z clikit.Zone, v eventView) error {
	_, err := io.WriteString(o.Out, eventSummary(v.event, v.timeline, z)+"\n")
	if err == nil {
		err = writeSection(o, "Annotations:", annotationRows(v.annotations, z))
	}
	if err == nil {
		err = writeSection(o, "Links:", v.linkRows)
	}

	return err
}

// writeSection writes heading with how many rows follow it, then the rows.
func writeSection(o clikit.Output, heading string, rows [][]string) error {
	var b strings.Builder
	clikit.WriteField(&b, heading, strconv.Itoa(len(rows)))
	_, err := io.WriteString(o.Out, b.String())
	if err != nil || len(rows) == 0 {
		return err
	}

	return o.Rows(rows)
}

// eventLinkRows returns a row per link of the event with eventID: kind, to or from, the event at
// the link's other end as timeline#seq, and that event's text.
func eventLinkRows(ctx context.Context, s *store.Tx, eventID string, links []timeline.Link) ([][]string, error) {
	rows := make([][]string, 0, len(links))
	for _, l := range links {
		direction, otherID := "to", l.ToEventID
		if l.ToEventID == eventID {
			direction, otherID = "from", l.FromEventID
		}
		other, err := s.Event(ctx, otherID)
		if err != nil {
			return nil, err
		}
		t, err := s.Timeline(ctx, other.TimelineID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, []string{string(l.Kind), direction, clikit.SeqRef(t, other), other.Text})
	}

	return rows, nil
}

// eventSummary describes e, in t, one field per line under a heading, with times in z.
func eventSummary(e timeline.Event, t timeline.Timeline, z clikit.Zone) string {
	ended := clikit.EndedText(e, z)
	var b strings.Builder
	fmt.Fprintf(&b, "Event #%d in timeline `%s`\n", e.Seq, t.Name)
	clikit.WriteField(&b, "ID:", e.ID)
	clikit.WriteField(&b, "Occurred:", fmt.Sprintf("%s (%s)", clikit.OccurredText(e, z), e.Precision))
	clikit.WriteField(&b, "Ended:", ended)
	clikit.WriteField(&b, "Typed as:", clikit.OrDash(e.OccurredRaw))
	clikit.WriteField(&b, "Source:", clikit.OrDash(e.Source))
	clikit.WriteField(&b, "Tags:", cmp.Or(strings.Join(e.Tags, ", "), "-"))
	clikit.WriteField(&b, "URL:", clikit.OrDash(e.URL))
	clikit.WriteField(&b, "Author:", clikit.OrDash(e.Author))
	clikit.WriteField(&b, "Created:", clikit.DisplayTime(e.CreatedAt, timeline.PrecisionSecond, z))
	clikit.WriteField(&b, "Updated:", clikit.DisplayTime(e.UpdatedAt, timeline.PrecisionSecond, z))
	clikit.WriteField(&b, "Text:", e.Text)

	return b.String()
}

// annotationRows returns a row per annotation: kind, created at in z, author, source and body.
func annotationRows(annotations []timeline.Annotation, z clikit.Zone) [][]string {
	rows := make([][]string, 0, len(annotations))
	for _, a := range annotations {
		rows = append(rows, []string{
			string(a.Kind), clikit.DisplayTime(a.CreatedAt, timeline.PrecisionSecond, z), clikit.OrDash(a.Author), clikit.OrDash(a.Source), a.Body,
		})
	}

	return rows
}
