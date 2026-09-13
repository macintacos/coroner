package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

const missingTextHint = "Pass the text, or - to read it from stdin:\n" +
	"  $ coroner event add \"Primary stepped down\" --at 2026-09-11T03:14:07Z\n" +
	"  $ pbpaste | coroner event add - --source slack\n" +
	"Run 'coroner event add --help' for more."

func newEventAddCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add {<text> | -}",
		Short: "Add an event to the current timeline",
		Long: "Add an event to the current timeline, and print the time it resolved to.\n\n" +
			"--at and --ended take RFC 3339, a date and time, a syslog timestamp, an epoch, or natural\n" +
			"language such as \"3 hours ago\". A time with no offset is read in --tz, then the config\n" +
			"file's default_timezone, then the local zone. RFC 3339 is stable; natural language may\n" +
			"resolve differently in a later version, so scripts should send RFC 3339.\n\n" +
			"Pass - to read the text from stdin, which is never read otherwise.",
		Args:        eventTextArg,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event add \"Primary stepped down: replSetStepDown\" --at 2026-09-11T03:14:07.412Z \\\n" +
			"      --source mongod --tag election\n" +
			"  $ pbpaste | coroner event add - --source slack --at \"yesterday 23:02\"\n" +
			"  $ coroner event add \"Deploy started\" --at \"around 14:00\" --ended 14:20 --dry-run --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return addEvent(ctx, cmd, app, args)
		},
	}

	flags := cmd.Flags()
	flags.String("at", "", "When it happened, as a `time` (default: now)")
	flags.String("ended", "", "When it ended, as a `time`, for an event that lasted")
	flags.Bool("approximate", false, "Mark the time as a guess, as a ~ or around prefix does")
	flags.String("source", "", "System, service or person it came from, created on first use")
	flags.StringArray("tag", nil, "Keyword such as phase:detect; repeat for more")
	flags.String("url", "", "Permalink to the log line or message")
	flags.String("author", "", "Who is adding it (default: config author, then USER)")
	flags.Bool("dry-run", false, "Show the event as it would be added, and add nothing")

	return cmd
}

func addEvent(ctx context.Context, cmd *cobra.Command, app App, args []string) error {
	flag, err := timelineFlag(cmd.Flags())
	if err != nil {
		return err
	}
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}
	cfg, err := resolveConfig(cmd, app)
	if err != nil {
		return err
	}
	draft, err := draftEvent(cmd.Flags(), app, cfg, args)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	return withStoreAt(ctx, app, cfg.DBPath, func(s *store.Store) error {
		// ponytail: the timeline is read in its own transaction, so one deleted before AddEvent
		// fails its foreign key as internal; look it up inside AddEvent's write if that matters.
		t, _, err := selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		draft.event.TimelineID = t.ID
		if dryRun {
			return previewEvent(o, t, draft)
		}

		added, err := s.AddEvent(ctx, draft.event, draft.newSourceID)
		if err != nil {
			return err
		}
		err = printEvent(o, added)
		if err != nil {
			return err
		}

		return o.notify("Added event #%d to timeline `%s` at %s", added.Seq, t.Name, draft.when)
	})
}

// eventDraft is an event read from the command line and checked, still without its timeline.
type eventDraft struct {
	event timeline.Event
	// newSourceID is the id event.Source gets when no source has that name yet.
	newSourceID string
	// when is the event's resolved time as the command prints it.
	when string
}

// draftEvent reads the event args and flags describe, rejecting anything the store would.
func draftEvent(flags *pflag.FlagSet, app App, cfg config.Config, args []string) (eventDraft, error) {
	text, err := eventText(args, app.In, app.InIsTTY)
	if err != nil {
		return eventDraft{}, err
	}
	e, err := readEventFields(flags, cfg.Author, app.LookupEnv)
	if err != nil {
		return eventDraft{}, err
	}
	now := app.Now()
	times, err := resolveEventTimes(flags, now, cfg.DefaultTimezone)
	if err != nil {
		return eventDraft{}, err
	}
	eventID, err := app.NewID()
	if err != nil {
		return eventDraft{}, err
	}
	sourceID, err := app.NewID()
	if err != nil {
		return eventDraft{}, err
	}

	e.ID, e.Text, e.CreatedAt, e.UpdatedAt = eventID.String(), text, now, now
	e.OccurredAt, e.OccurredOffset, e.Precision = times.at.Instant, times.at.Offset, times.at.Precision
	e.Approximate, e.OccurredRaw, e.EndedAt = times.at.Approximate, changedString(flags, "at"), times.ended
	err = e.Validate()
	if err != nil {
		return eventDraft{}, &Error{Kind: KindUsage, Message: err.Error(), Err: err}
	}

	return eventDraft{event: e, newSourceID: sourceID.String(), when: times.when}, nil
}

// eventTextArg rejects more than one argument, which is most often event text left unquoted.
func eventTextArg(_ *cobra.Command, args []string) error {
	if len(args) <= 1 {
		return nil
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("event add takes the event text as one argument, got %d", len(args)),
		Hint:    "Quote text that has spaces: coroner event add \"Primary stepped down\"",
	}
}

// eventText returns the text args give, reading in when that is -. It never reads a terminal.
func eventText(args []string, in io.Reader, inIsTTY bool) (string, error) {
	switch {
	case len(args) == 0:
		return "", &Error{Kind: KindUsage, Message: "event add needs the event's text", Hint: missingTextHint}
	case args[0] != "-":
		return checkText(args[0])
	case inIsTTY:
		return "", &Error{
			Kind:    KindUsage,
			Message: "- reads the event text from stdin, which is a terminal",
			Hint:    "Pipe the text in, as in: pbpaste | coroner event add -",
		}
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read the event text from stdin: %w", err)
	}

	return checkText(strings.TrimSuffix(string(data), "\n"))
}

func checkText(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", &Error{Kind: KindUsage, Message: "event text must not be blank", Hint: missingTextHint}
	}

	return text, nil
}

// readEventFields returns an event holding the source, tags, URL and author the flags give,
// with the author defaulting to configuredAuthor, then USER.
func readEventFields(
	flags *pflag.FlagSet, configuredAuthor string, lookupEnv func(string) (string, bool),
) (timeline.Event, error) {
	e := timeline.Event{
		Source: changedString(flags, "source"),
		Tags:   appendUnique(nil, stringArray(flags, "tag")...),
		URL:    changedString(flags, "url"),
		Author: changedString(flags, "author"),
	}

	err := checkNotBlank("--url", e.URL)
	if err != nil {
		return e, err
	}
	err = checkNotBlank("--author", e.Author)
	if err != nil {
		return e, err
	}
	err = checkSlugs(e.Source, e.Tags)
	if err != nil {
		return e, err
	}

	e.Author = eventAuthor(e.Author, configuredAuthor, lookupEnv)

	return e, nil
}

func checkNotBlank(flag string, value *string) error {
	if value == nil || strings.TrimSpace(*value) != "" {
		return nil
	}

	return &Error{Kind: KindUsage, Message: flag + " must not be blank", Hint: "Leave " + flag + " out instead."}
}

// checkSlugs returns invalid_name, with a suggestion, for the first of source and tags that
// breaks their grammar.
func checkSlugs(source *string, tags []string) error {
	if source != nil {
		err := checkSlug(*source, timeline.ValidateSourceName)
		if err != nil {
			return err
		}
	}

	for _, tag := range tags {
		err := checkSlug(tag, timeline.ValidateTag)
		if err != nil {
			return err
		}
	}

	return nil
}

func checkSlug(slug string, validate func(string) error) error {
	err := validate(slug)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_', ':', '/' and '-'."
	if suggestion := suggestSlug(slug, sourceTagPunct, validate); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`?", suggestion)
	}

	return &Error{Kind: KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// eventAuthor returns flag, else configured, else USER, or nil when all are empty.
func eventAuthor(flag *string, configured string, lookupEnv func(string) (string, bool)) *string {
	if flag != nil {
		return flag
	}

	user, _ := lookupEnv("USER")
	if author := cmp.Or(configured, user); author != "" {
		return &author
	}

	return nil
}

// eventTimes are an event's resolved --at and --ended.
type eventTimes struct {
	at    timeparse.Resolved
	ended *time.Time
	// when is at, and ended when given, as the command prints them.
	when string
}

// resolveEventTimes resolves --at and --ended as of now, in the zone --tz, then
// defaultTimezone, then now's own location select.
func resolveEventTimes(flags *pflag.FlagSet, now time.Time, defaultTimezone string) (eventTimes, error) {
	local := now.Location()
	loc, err := timeLocation(changedString(flags, "tz"), defaultTimezone, local)
	if err != nil {
		return eventTimes{}, err
	}

	at, err := resolveTime("--at", changedString(flags, "at"), now, loc)
	if err != nil {
		return eventTimes{}, err
	}
	approximate, _ := flags.GetBool("approximate")
	at.Approximate = at.Approximate || approximate
	times := eventTimes{at: at, when: formatResolved(at, loc, local)}

	endedInput := changedString(flags, "ended")
	if endedInput == nil {
		return times, nil
	}
	ended, err := resolveTime("--ended", endedInput, now, loc)
	if err != nil {
		return eventTimes{}, err
	}
	if ended.Instant.Before(at.Instant) {
		return eventTimes{}, &Error{
			Kind:    KindInvalidTime,
			Message: fmt.Sprintf("--ended %s is before --at %s", formatResolved(ended, loc, local), times.when),
			Hint:    "Pass an --ended time no earlier than --at.",
		}
	}

	times.ended = &ended.Instant
	times.when += ", ended " + formatResolved(ended, loc, local)

	return times, nil
}

// printEvent prints e when o asks for JSON; text output leaves it to the command's message.
func printEvent(o output, e timeline.Event) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newEventJSON(e))
}

// previewEvent prints d as it would be added to t: its JSON object, or a description.
func previewEvent(o output, t timeline.Timeline, d eventDraft) error {
	if o.format == formatJSON {
		return o.json(newEventJSON(d.event))
	}

	e := d.event
	var b strings.Builder
	fmt.Fprintf(&b, "Would add to timeline `%s` at %s\n", t.Name, d.when)
	writeField(&b, "Precision:", string(e.Precision))
	writeField(&b, "Source:", orDash(e.Source))
	writeField(&b, "Tags:", cmp.Or(strings.Join(e.Tags, ", "), "-"))
	writeField(&b, "URL:", orDash(e.URL))
	writeField(&b, "Author:", orDash(e.Author))
	writeField(&b, "Text:", e.Text)

	_, err := io.WriteString(o.out, b.String())
	return err
}

func orDash(s *string) string {
	if s == nil {
		return "-"
	}

	return *s
}
