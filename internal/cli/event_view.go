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

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventViewCommand(ctx context.Context, app App) *cobra.Command {
	return &cobra.Command{
		Use:   "view <event>",
		Short: "Show an event, its annotations and its links",
		Long: "Show every field of an event, then its annotations, then its links to and from other events\n" +
			"in any timeline. --json prints the event object with its links.\n\n" + eventRefHelp,
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
	parsed, err := parseEventRef(ref)
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

	return withStorePath(ctx, app, inv.cfg.DBPath, func(s *store.Store) error {
		e, t, err := findEvent(ctx, s, parsed, inv.timelines, app.LookupEnv)
		if err != nil {
			return err
		}
		links, err := s.EventLinks(ctx, e.ID)
		if err != nil {
			return err
		}
		if inv.out.format == formatJSON {
			attachments, err := s.ListAttachments(ctx, timeline.AttachmentOwner{EventID: &e.ID})
			if err != nil {
				return err
			}
			return inv.out.json(eventViewJSON{eventJSON: newEventJSON(e, newAttachmentJSONs(attachments, inv.cfg.DBPath)), Links: newLinkJSONs(links)})
		}

		annotations, err := s.EventAnnotations(ctx, e.ID)
		if err != nil {
			return err
		}
		linkRows, err := eventLinkRows(ctx, s, e.ID, links)
		if err != nil {
			return err
		}

		return writeEventView(inv.out, z, eventView{event: e, timeline: t, annotations: annotations, linkRows: linkRows})
	})
}

// eventViewJSON is event view's object: the event's, with its links in either direction.
type eventViewJSON struct {
	eventJSON

	// Never nil, so no links encode as [] rather than null.
	Links []export.Link `json:"links"`
}

func newLinkJSONs(links []timeline.Link) []export.Link {
	objects := make([]export.Link, 0, len(links))
	for _, l := range links {
		objects = append(objects, newLinkJSON(l))
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
func writeEventView(o output, z zone, v eventView) error {
	_, err := io.WriteString(o.out, eventSummary(v.event, v.timeline, z)+"\n")
	if err == nil {
		err = writeSection(o, "Annotations:", annotationRows(v.annotations, z))
	}
	if err == nil {
		err = writeSection(o, "Links:", v.linkRows)
	}

	return err
}

// writeSection writes heading with how many rows follow it, then the rows.
func writeSection(o output, heading string, rows [][]string) error {
	var b strings.Builder
	writeField(&b, heading, strconv.Itoa(len(rows)))
	_, err := io.WriteString(o.out, b.String())
	if err != nil || len(rows) == 0 {
		return err
	}

	return o.rows(rows)
}

// eventLinkRows returns a row per link of the event with eventID: kind, to or from, the event at
// the link's other end as timeline#seq, and that event's text.
func eventLinkRows(ctx context.Context, s *store.Store, eventID string, links []timeline.Link) ([][]string, error) {
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
		rows = append(rows, []string{string(l.Kind), direction, seqRef(t, other), other.Text})
	}

	return rows, nil
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
