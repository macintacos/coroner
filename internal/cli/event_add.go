package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

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
	def := eventAddDefinition()
	cmd := &cobra.Command{
		Use:   "add {<text> | -}",
		Short: "Add an event to the current timeline",
		Long: "Add an event to the current timeline, and print the time it resolved to.\n\n" +
			"A time with no offset is read in --tz, then the config file's default_timezone, then the\n" +
			"local zone. RFC 3339 is stable and natural language experimental, so scripts send RFC 3339.\n" +
			"Run 'coroner help time' for the times you can type.\n\n" +
			"Pass - to read the text from stdin, which is never read otherwise.",
		Args:        eventTextArg,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event add \"Primary stepped down: replSetStepDown\" --at 2026-09-11T03:14:07.412Z \\\n" +
			"      --source mongod --tag election\n" +
			"  $ pbpaste | coroner event add - --source slack --at \"yesterday 23:02\"\n" +
			"  $ coroner event add \"Deploy started\" --at \"around 2026-09-11 14:00\" \\\n" +
			"      --ended \"2026-09-11 14:20\" --dry-run --json",
		RunE: writeRunE(ctx, app, def, addEvent),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func eventAddDefinition() definition {
	return definition{fields: []field{
		inherited(timelineField()),
		{name: "text", arg: true, usage: "What happened, as `text`, or - to read it from stdin", check: withoutEnv(checkTextValue)},
		{name: "at", usage: "When it happened, as a `time` (default: now)", check: checkAt},
		{
			name: "ended", usage: "When it ended, as a `time`, for an event that lasted",
			check: func(ended string, env checkEnv) error {
				_, err := resolveEnded(ended, env, addedEndedHint)
				return err
			},
		},
		{name: "approximate", typ: boolField, usage: "Mark the time as a guess, as a ~ or around prefix does"},
		{name: "source", usage: "System, service or person it came from, as a `source`, created on first use", check: withoutEnv(checkSource)},
		{name: "tag", typ: stringsField, usage: "Keyword such as phase:detect, as a `tag`; repeat for more", check: withoutEnv(checkTag)},
		{name: "url", usage: "Permalink to the log line or message, as a `url`", check: notBlank("--url")},
		{name: "author", usage: "Who is adding it, as a `name` (default: config author, then USER)", check: notBlank("--author")},
		{name: "dry-run", typ: boolField, usage: "Show the event as it would be added, and add nothing"},
		tzField(),
	}}
}

const addedEndedHint = "~ and around mark --at only; pass --approximate to mark the event."

// eventAdded is the event add stored or, with --dry-run, would store, in its timeline.
type eventAdded struct {
	event    timeline.Event
	timeline timeline.Timeline
	// when is the event's resolved time as the command prints it.
	when   string
	dryRun bool
}

func addEvent(ctx context.Context, app App, cfg config.Config, in fieldValues) (eventAdded, error) {
	now := app.Now()
	z, err := timeZone(in.value("tz"), cfg.DefaultTimezone, now.Location())
	if err != nil {
		return eventAdded{}, err
	}
	env := checkEnv{now: now, zone: z}
	err = eventAddDefinition().check(in, env)
	if err != nil {
		return eventAdded{}, err
	}
	draft, err := draftEvent(in, app, cfg, env)
	if err != nil {
		return eventAdded{}, err
	}
	dryRun, _ := in.boolean("dry-run")

	added := eventAdded{event: draft.event, when: draft.when, dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		// ponytail: the timeline is read in its own transaction, so one deleted before AddEvent
		// fails AllocateSeq as internal; look it up inside AddEvent's write if that matters.
		t, err := selectTimeline(ctx, s, in.value("timeline"), app.LookupEnv)
		if err != nil {
			return err
		}
		added.timeline = t
		added.event.TimelineID = t.ID
		if dryRun {
			return nil
		}

		added.event, err = s.AddEvent(ctx, added.event, draft.newSourceID)
		return err
	})

	return added, err
}

func (a eventAdded) render(o output) error {
	if a.dryRun {
		return previewEvent(o, a.timeline, a.event, a.when)
	}

	err := printEvent(o, a.event)
	if err != nil {
		return err
	}

	return o.notify("Added event #%d to timeline `%s` at %s", a.event.Seq, a.timeline.Name, a.when)
}

// eventDraft is an event read from its fields and checked, still without its timeline.
type eventDraft struct {
	event timeline.Event
	// newSourceID is the id event.Source gets when no source has that name yet.
	newSourceID string
	// when is the event's resolved time as the command prints it.
	when string
}

// draftEvent returns the event in describes, with times read as of env, and gives it new ids,
// rejecting anything the store would. It reads stdin last, so a bad field never waits on it.
func draftEvent(in fieldValues, app App, cfg config.Config, env checkEnv) (eventDraft, error) {
	fields := readEventFields(in, cfg.Author, app.LookupEnv)
	times, err := resolveEventTimes(in, env)
	if err != nil {
		return eventDraft{}, err
	}
	text, err := eventText(in.values("text"), app.In, app.InIsTTY)
	if err != nil {
		return eventDraft{}, err
	}
	eventID, err := app.NewID()
	if err != nil {
		return eventDraft{}, err
	}
	sourceID, err := newSourceID(fields.source, app.NewID)
	if err != nil {
		return eventDraft{}, err
	}

	e := timeline.Event{
		ID:             eventID.String(),
		OccurredAt:     times.at.Instant,
		OccurredOffset: times.at.Offset,
		Precision:      times.at.Precision,
		Approximate:    times.at.Approximate,
		OccurredRaw:    times.raw,
		EndedAt:        times.ended,
		Source:         fields.source,
		Tags:           fields.tags,
		Text:           text,
		URL:            fields.url,
		Author:         fields.author,
		CreatedAt:      env.now,
		UpdatedAt:      env.now,
	}
	err = e.Validate()
	if err != nil {
		return eventDraft{}, &Error{
			Kind: KindUsage, Message: err.Error(), Hint: "Run 'coroner event add --help' for what each flag takes.", Err: err,
		}
	}

	return eventDraft{event: e, newSourceID: sourceID, when: times.when}, nil
}

// newSourceID returns a new id for source, or "" when no source was given.
func newSourceID(source *string, newID func() (uuid.UUID, error)) (string, error) {
	if source == nil {
		return "", nil
	}

	id, err := newID()
	return id.String(), err
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

// checkTextValue checks event text as typed, where - stands for text read from stdin later.
func checkTextValue(text string) error {
	if text == "-" {
		return nil
	}

	_, err := checkText(text)
	return err
}

func checkText(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", &Error{Kind: KindUsage, Message: "event text must not be blank", Hint: "Pass non-blank text."}
	}
	// JSON would replace invalid bytes, so an export could never match what is stored.
	if !utf8.ValidString(text) {
		return "", &Error{
			Kind: KindUsage, Message: "event text must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8",
		}
	}

	return text, nil
}

// eventFields are the source, tags, URL and author an event's fields give it.
type eventFields struct {
	source, url, author *string
	tags                []string
}

// readEventFields returns the event fields in gives, with the author defaulting to
// configuredAuthor, then USER.
func readEventFields(in fieldValues, configuredAuthor string, lookupEnv func(string) (string, bool)) eventFields {
	return eventFields{
		source: in.value("source"),
		tags:   appendUnique(nil, in.values("tag")...),
		url:    in.value("url"),
		author: eventAuthor(in.value("author"), configuredAuthor, lookupEnv),
	}
}

func checkNotBlank(flag string, value *string) error {
	if value == nil || strings.TrimSpace(*value) != "" {
		return nil
	}

	return &Error{Kind: KindUsage, Message: flag + " must not be blank", Hint: "Leave " + flag + " out instead."}
}

// notBlank returns a field check that rejects a blank value of flag.
func notBlank(flag string) func(string, checkEnv) error {
	return withoutEnv(func(value string) error {
		return checkNotBlank(flag, &value)
	})
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

func checkSource(source string) error {
	return checkSlug(source, timeline.ValidateSourceName)
}

func checkTag(tag string) error {
	return checkSlug(tag, timeline.ValidateTag)
}

func checkSlug(slug string, validate func(string) error) error {
	err := validate(slug)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_', ':', '/' and '-'."
	if slug == clearKeyword {
		hint = "none clears an event's source in event edit, so no source can be called it; pick another name."
	} else if suggestion := suggestSlug(slug, sourceTagPunct, validate); suggestion != "" {
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
	at timeparse.Resolved
	// raw is --at as given, or nil when it was not.
	raw   *string
	ended *time.Time
	// when is at, and ended when given, as the command prints them.
	when string
}

// resolveEventTimes resolves --at (marked approximate by --approximate) and --ended as of env,
// rejecting an --ended that is approximate or before --at.
func resolveEventTimes(in fieldValues, env checkEnv) (eventTimes, error) {
	raw := in.value("at")
	at, err := resolveTime("--at", raw, env.now, env.zone.loc)
	if err != nil {
		return eventTimes{}, err
	}
	approximate, _ := in.boolean("approximate")
	at.Approximate = at.Approximate || approximate
	times := eventTimes{at: at, raw: raw, when: formatResolved(at, env.zone)}

	endedInput := in.value("ended")
	if endedInput == nil {
		return times, nil
	}
	ended, err := resolveEnded(*endedInput, env, addedEndedHint)
	if err != nil {
		return eventTimes{}, err
	}
	if ended.Instant.Before(at.Instant) {
		return eventTimes{}, &Error{
			Kind:    KindInvalidTime,
			Message: fmt.Sprintf("--ended %s is before --at %s", formatResolved(ended, env.zone), times.when),
			Hint:    "Pass an --ended time no earlier than --at.",
		}
	}

	times.ended = &ended.Instant
	times.when += ", ended " + formatResolved(ended, env.zone)

	return times, nil
}

// printEvent prints e when o asks for JSON; text output leaves it to the command's message.
func printEvent(o output, e timeline.Event) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newEventJSON(e))
}

// previewEvent prints e as it would be added to t at when: its JSON object, or a description.
func previewEvent(o output, t timeline.Timeline, e timeline.Event, when string) error {
	if o.format == formatJSON {
		return o.json(newEventJSON(e))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Would add to timeline `%s` at %s\n", t.Name, when)
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
