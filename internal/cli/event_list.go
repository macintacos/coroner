package cli

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newEventListCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a timeline's events, filtered",
		Long: "List the current timeline's events in the order they happened: by occurred at, then seq.\n\n" +
			"--since is inclusive and --until exclusive. Repeated --source flags match any of them,\n" +
			"and repeated --tag flags must all match. Times with no offset are read, and times print,\n" +
			"in --tz, then the config file's default_timezone, then the local zone. Piped, the\n" +
			"fields are tab-separated.",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		Example: "  $ coroner event list --tag election --source mongod\n" +
			"  $ coroner event list --since \"2026-09-11T03:00Z\" --until \"2026-09-11T04:00Z\" --tag election --format jsonl\n" +
			"  $ coroner event list --contains stepdown --order desc --limit 20",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listEvents(ctx, cmd, app)
		},
	}

	addEventFilterFlags(cmd.Flags())

	return cmd
}

// addEventFilterFlags declares the flags that filter a timeline's events, which mean the
// same on every command that takes them.
func addEventFilterFlags(flags *pflag.FlagSet) {
	flags.String("since", "", "Keep events at or after this `time`")
	flags.String("until", "", "Keep events before this `time`")
	addEventMatchFlags(flags)
	flags.String("order", "asc", "Order by occurred at, then seq, as an `order`: {asc|desc}")
	flags.Int("limit", 0, "Show at most this `number` of events (default: every one)")
}

// addEventMatchFlags declares the flags that pick events by source, tag and text.
func addEventMatchFlags(flags *pflag.FlagSet) {
	flags.StringArray("source", nil, "Keep events from this `source`; repeat to match any of several")
	flags.StringArray("tag", nil, "Keep events with this `tag`; repeat to require all of them")
	flags.String("contains", "", "Keep events whose text contains this `text`, ignoring case")
}

func listEvents(ctx context.Context, cmd *cobra.Command, app App) error {
	flag, err := timelineFlag(cmd.Flags())
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
	filter, bounds, err := readEventFilter(cmd.Flags(), app.Now(), z)
	if err != nil {
		return err
	}

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		t, err := selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		filter.TimelineIDs = []string{t.ID}
		page, err := s.ListEvents(ctx, filter)
		if err != nil {
			return err
		}
		listing := eventListing{page: page}
		if o.format.machine() {
			attachments, err := s.TimelineAttachments(ctx, t.ID)
			if err != nil {
				return err
			}
			listing.attached = attachmentsByOwner(attachments, cfg.DBPath)
		}
		if bounds != "" {
			err = o.notify("Events %s", bounds)
			if err != nil {
				return err
			}
		}

		return printEventPage(o, z, listing, noEventsMessage(filter))
	})
}

// noEventsMessage returns what text output says when f selects no events.
func noEventsMessage(f store.EventFilter) string {
	if f.Since != nil || f.Until != nil || len(f.Sources) > 0 || len(f.Tags) > 0 || f.Contains != "" || f.Limit > 0 {
		return "No events match."
	}

	return "No events yet. Add one with: coroner event add <text>"
}

// readEventFilter returns the filter the flags ask for, and the times --since and --until
// resolved to as the command echoes them, or "" when neither was given. It rejects a filter
// before the store opens.
func readEventFilter(flags *pflag.FlagSet, now time.Time, z zone) (store.EventFilter, string, error) {
	f, err := readEventMatch(flags)
	if err != nil {
		return f, "", err
	}
	f.Order, err = readOrder(flags)
	if err != nil {
		return f, "", err
	}
	f.Limit, err = readLimit(flags)
	if err != nil {
		return f, "", err
	}

	var since, until string
	f.Since, since, err = readBound(flags, "since", now, z)
	if err != nil {
		return f, "", err
	}
	f.Until, until, err = readBound(flags, "until", now, z)

	return f, strings.TrimSpace(since + " " + until), err
}

// readEventMatch returns the sources, tags and text the match flags keep events by, rejecting
// them before the store opens.
func readEventMatch(flags *pflag.FlagSet) (store.EventFilter, error) {
	f := store.EventFilter{
		Sources: appendUnique(nil, stringArray(flags, "source")...),
		Tags:    appendUnique(nil, stringArray(flags, "tag")...),
	}

	err := checkFilterSlugs(f)
	if err != nil {
		return f, err
	}
	f.Contains, err = readContains(flags)

	return f, err
}

func checkFilterSlugs(f store.EventFilter) error {
	for _, source := range f.Sources {
		err := checkSlug(source, timeline.ValidateSourceName)
		if err != nil {
			return err
		}
	}

	return checkSlugs(nil, f.Tags)
}

func readContains(flags *pflag.FlagSet) (string, error) {
	contains := changedString(flags, "contains")
	if contains == nil {
		return "", nil
	}

	return *contains, checkNotBlank("--contains", contains)
}

func readOrder(flags *pflag.FlagSet) (store.Order, error) {
	order, _ := flags.GetString("order")
	switch order {
	case "asc":
		return store.OrderAsc, nil
	case "desc":
		return store.OrderDesc, nil
	}

	return store.OrderAsc, &Error{
		Kind: KindUsage, Message: fmt.Sprintf("--order %q is not asc or desc", order), Hint: "Pass --order asc or --order desc.",
	}
}

func readLimit(flags *pflag.FlagSet) (int, error) {
	limit, _ := flags.GetInt("limit")
	if flags.Changed("limit") && limit < 1 {
		return 0, &Error{
			Kind: KindUsage, Message: fmt.Sprintf("--limit %d is below 1", limit), Hint: "Leave --limit out to list every event.",
		}
	}

	return limit, nil
}

// readBound resolves the time flag name gives, and how the command echoes it; the time is
// nil and the echo "" when the flag was not given.
func readBound(flags *pflag.FlagSet, name string, now time.Time, z zone) (*time.Time, string, error) {
	input := changedString(flags, name)
	if input == nil {
		return nil, "", nil
	}

	r, err := resolveTime("--"+name, input, now, z.loc)
	if err != nil {
		return nil, "", err
	}

	return &r.Instant, name + " " + formatResolved(r, z), nil
}

type eventListJSON struct {
	// Never nil, so no events encode as [] rather than null.
	Events    []eventJSON `json:"events"`
	Truncated bool        `json:"truncated"`
	Total     int         `json:"total"`
}

// eventLineJSON is an event as a JSONL line, whose type says what the line holds.
type eventLineJSON struct {
	Type string `json:"type"`

	eventJSON
}

// eventListing is a page of events with their attachments' objects, keyed by event id.
type eventListing struct {
	page     store.EventPage
	attached map[string][]attachmentJSON
}

func newEventListJSON(l eventListing) eventListJSON {
	events := make([]eventJSON, 0, len(l.page.Events))
	for _, e := range l.page.Events {
		events = append(events, newEventJSON(e, l.attached[e.ID]))
	}

	return eventListJSON{Events: events, Truncated: l.page.Truncated, Total: l.page.Total}
}

// printEventPage prints l in o's format, with text times in z and noEvents on stderr when it
// is empty, then says on stderr when its limit left events out.
func printEventPage(o output, z zone, l eventListing, noEvents string) error {
	err := writeEventPage(o, z, l, noEvents)
	if err != nil {
		return err
	}

	return notifyTruncated(o, l.page)
}

// notifyTruncated says on stderr when page's limit left events out.
func notifyTruncated(o output, page store.EventPage) error {
	if !page.Truncated {
		return nil
	}

	return o.notify("Showing %d of %d events; raise --limit, or leave it out, to see them all.", len(page.Events), page.Total)
}

func writeEventPage(o output, z zone, l eventListing, noEvents string) error {
	page := l.page
	switch o.format {
	case formatJSON:
		return o.json(newEventListJSON(l))
	case formatJSONL:
		for _, e := range page.Events {
			err := writeJSON(o.out, false, eventLineJSON{Type: "event", eventJSON: newEventJSON(e, l.attached[e.ID])})
			if err != nil {
				return err
			}
		}

		return nil
	case formatMarkdown, formatMermaid, formatHTML, formatSVG:
		return fmt.Errorf("event list cannot write --format %s", o.format)
	case formatText:
	}

	if len(page.Events) == 0 {
		return o.notify(noEvents)
	}

	return o.rows(eventListRows(page.Events, z))
}

// eventListRows returns a row per event: seq, occurred at in z, source, tags and text.
func eventListRows(events []timeline.Event, z zone) [][]string {
	rows := make([][]string, 0, len(events))
	for _, e := range events {
		rows = append(rows, []string{
			"#" + strconv.FormatInt(e.Seq, 10), occurredText(e, z), orDash(e.Source), cmp.Or(strings.Join(e.Tags, ","), "-"), e.Text,
		})
	}

	return rows
}
