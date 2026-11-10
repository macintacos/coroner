package event

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventListCommand(ctx context.Context, app clikit.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a timeline's events, filtered",
		Long: "List the current timeline's events in the order they happened: by occurred at, then seq.\n\n" +
			"Repeat -t to overlay several timelines: their events merge into one list by occurred at,\n" +
			"then timeline id, then seq, and each text row starts with its timeline's name. --json lists\n" +
			"the selected timelines under timelines, and --format jsonl starts with a timeline line for\n" +
			"each, so an event's timeline_id names one of them. --overlay <name> reads the timelines an\n" +
			"overlay saved with 'coroner overlay create' holds, in place of -t.\n\n" +
			"--since is inclusive and --until exclusive. Repeated --source flags match any of them,\n" +
			"and repeated --tag flags must all match. Times with no offset are read, and times print,\n" +
			"in --tz, then the config file's default_timezone, then UTC. Piped, the\n" +
			"fields are tab-separated.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json,jsonl", clikit.TimelinesAnnotation: "several"},
		Example: "  $ coroner event list --tag election --source mongod\n" +
			"  $ coroner event list --since \"2026-09-11T03:00Z\" --until \"2026-09-11T04:00Z\" --tag election --format jsonl\n" +
			"  $ coroner event list --contains stepdown --order desc --limit 20\n" +
			"  $ coroner event list -t mongo-failover -t checkout-latency --since 2026-09-10T14:00Z --json\n" +
			"  $ coroner event list --overlay checkout-incident --tag phase:detect",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listEvents(ctx, cmd, app)
		},
	}

	clikit.AddEventFilterFlags(cmd.Flags())
	clikit.AddOverlayFlag(cmd.Flags())

	return cmd
}

func listEvents(ctx context.Context, cmd *cobra.Command, app clikit.App) error {
	inv, z, err := clikit.ReadInvocationInZone(cmd, app)
	if err != nil {
		return err
	}
	filter, bounds, err := clikit.ReadEventFilter(app.Logger(), cmd.Flags(), app.Now(), z)
	if err != nil {
		return err
	}

	return clikit.WithStorePath(ctx, app, inv.Cfg.DBPath, func(s *store.Store) error {
		sel, err := clikit.SelectTimelines(ctx, app.Logger(), s, inv.Timelines, app.LookupEnv)
		if err != nil {
			return err
		}
		timelines, err := sel.All(ctx, s)
		if err != nil {
			return err
		}
		filter.TimelineIDs = clikit.TimelineIDs(timelines)
		page, err := s.ListEvents(ctx, filter)
		if err != nil {
			return err
		}
		clikit.LogRead(app.Logger(), clikit.CountOf("events", len(page.Events)), slog.Any("filter", clikit.FilterValue(filter)))
		listing := eventListing{timelines: timelines, page: page}
		if inv.Out.Format.Machine() {
			attachments, err := clikit.ReadEach(timelines, func(id string) ([]timeline.Attachment, error) {
				return s.TimelineAttachments(ctx, id)
			})
			if err != nil {
				return err
			}
			listing.attached = clikit.AttachmentsByOwner(attachments, inv.Cfg.DBPath)
		}
		if bounds != "" {
			err = inv.Out.Notify("Events %s", bounds)
			if err != nil {
				return err
			}
		}

		return printEventPage(inv.Out, z, listing, noEventsMessage(filter))
	})
}

// noEventsMessage returns what text output says when f selects no events.
func noEventsMessage(f store.EventFilter) string {
	if f.Since != nil || f.Until != nil || len(f.Sources) > 0 || len(f.Tags) > 0 || f.Contains != "" {
		return "No events match."
	}

	return "No events yet. Add one with: coroner event add <text>"
}

type eventListJSON struct {
	// Timelines are the timelines the events are listed from. Never nil, like Events.
	Timelines []export.Timeline `json:"timelines"`
	// Never nil, so no events encode as [] rather than null.
	Events    []JSON `json:"events"`
	Truncated bool   `json:"truncated"`
	Total     int    `json:"total"`
}

// eventLineJSON is an event as a JSONL line, whose type says what the line holds.
type eventLineJSON struct {
	Type string `json:"type"`

	JSON
}

// timelineLineJSON is a timeline as a JSONL line, whose type says what the line holds.
type timelineLineJSON struct {
	Type string `json:"type"`

	export.Timeline
}

// eventListing is a page of events from timelines, with their attachments' objects, keyed by
// event id.
type eventListing struct {
	timelines []timeline.Timeline
	page      store.EventPage
	attached  map[string][]clikit.AttachmentJSON
}

func newEventListJSON(l eventListing) eventListJSON {
	timelines := make([]export.Timeline, 0, len(l.timelines))
	for _, t := range l.timelines {
		timelines = append(timelines, clikit.NewTimelineJSON(t))
	}
	events := make([]JSON, 0, len(l.page.Events))
	for _, e := range l.page.Events {
		events = append(events, NewJSON(e, l.attached[e.ID]))
	}

	return eventListJSON{Timelines: timelines, Events: events, Truncated: l.page.Truncated, Total: l.page.Total}
}

// printEventPage prints l in o's format, with text times in z and noEvents on stderr when it
// is empty, then says on stderr when its limit left events out.
func printEventPage(o clikit.Output, z clikit.Zone, l eventListing, noEvents string) error {
	err := writeEventPage(o, z, l, noEvents)
	if err != nil {
		return err
	}

	return clikit.NotifyTruncated(o, l.page)
}

func writeEventPage(o clikit.Output, z clikit.Zone, l eventListing, noEvents string) error {
	page := l.page
	switch o.Format {
	case clikit.FormatJSON:
		return o.JSON(newEventListJSON(l))
	case clikit.FormatJSONL:
		return writeEventLines(o, l)
	case clikit.FormatMarkdown, clikit.FormatMermaid, clikit.FormatHTML, clikit.FormatSVG:
		return fmt.Errorf("event list cannot write --format %s", o.Format)
	case clikit.FormatText:
	}

	if len(page.Events) == 0 {
		return o.Notify(noEvents)
	}
	rows := eventListRows(page.Events, z)
	if len(l.timelines) > 1 {
		rows = clikit.WithTimelineNames(rows, page.Events, l.timelines)
	}

	return o.Rows(rows)
}

// writeEventLines writes a JSONL line for each of l's timelines, then for each of its events.
func writeEventLines(o clikit.Output, l eventListing) error {
	for _, t := range l.timelines {
		err := clikit.WriteJSON(o.Out, false, timelineLineJSON{Type: "timeline", Timeline: clikit.NewTimelineJSON(t)})
		if err != nil {
			return err
		}
	}
	for _, e := range l.page.Events {
		err := clikit.WriteJSON(o.Out, false, eventLineJSON{Type: "event", JSON: NewJSON(e, l.attached[e.ID])})
		if err != nil {
			return err
		}
	}

	return nil
}

// eventListRows returns a row per event: seq, occurred at in z, source, tags and text.
func eventListRows(events []timeline.Event, z clikit.Zone) [][]string {
	rows := make([][]string, 0, len(events))
	for _, e := range events {
		rows = append(rows, []string{
			"#" + strconv.FormatInt(e.Seq, 10), clikit.OccurredText(e, z), clikit.OrDash(e.Source), cmp.Or(strings.Join(e.Tags, ","), "-"), e.Text,
		})
	}

	return rows
}
