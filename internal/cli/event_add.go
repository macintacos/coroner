package cli

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

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
	cmd := &cobra.Command{
		Use:   "add {<text> | - | --from-file {<file> | -}}",
		Short: "Add an event to the current timeline",
		Long: "Add an event to the current timeline, and print the time it resolved to.\n\n" +
			"A time with no offset is read in --tz, then the config file's default_timezone, then the\n" +
			"local zone. RFC 3339 is stable and natural language experimental, so scripts send RFC 3339.\n" +
			"Run 'coroner help time' for the times you can type.\n\n" +
			"Pass - to read the text from stdin, which is never read otherwise.\n\n" +
			"Each --attach copies a file, such as a screenshot, into the blob directory beside the\n" +
			"database, and prints its id, sha256, size and type. Every file is read before anything is\n" +
			"added, so one that cannot be read adds nothing.\n\n" +
			"--id with --at, and --author when USER may differ, gives the event an id and time of your\n" +
			"own, so running the same command again adds nothing and exits 0, while the same id with\n" +
			"different content exits 4.\n\n" +
			"--from-file adds a JSONL file of event records, as 'coroner timeline export --format jsonl'\n" +
			"writes them, in one transaction, to the timeline -t, CORONER_TIMELINE or 'coroner timeline\n" +
			"use' selects, which every record's timeline_id must name. Every record is checked first, so\n" +
			"one bad record adds nothing. A record carries its own id and RFC 3339 times, so running the\n" +
			"same file again adds nothing.\n\n" +
			"On a terminal, with no text or with --interactive, a form asks for each flag, previews the\n" +
			"time as you type it, and prints the command each entry equals. Under --json only --interactive\n" +
			"opens it, and --no-input never does.",
		Args:        eventTextArg,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event add \"Primary stepped down: replSetStepDown\" --at 2026-09-11T03:14:07.412Z \\\n" +
			"      --source mongod --tag election\n" +
			"  $ coroner event add \"Dashboard at peak\" --at 14:02 --attach ./p99.png --attach ./cpu.png\n" +
			"  $ pbpaste | coroner event add - --source slack --at \"yesterday 23:02\"\n" +
			"  $ coroner event add \"Deploy started\" --at \"around 2026-09-11 14:00\" \\\n" +
			"      --ended \"2026-09-11 14:20\" --dry-run --json\n" +
			"  $ coroner event add \"Election timeout on db-3\" --id 01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2 \\\n" +
			"      --at 2026-09-11T03:14:30Z --json\n" +
			"  $ coroner event add -t mongo-outage --from-file events.jsonl --dry-run --json\n" +
			"  $ coroner event add --interactive --source mongod",
	}

	eventAddForm().attach(ctx, cmd, app, terminalForms(app))
	cmd.Flags().String("from-file", "", fromFileUsage)
	// A file of records needs no form, so --from-file branches before the form gate and stays
	// outside the definition.
	withForm := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("from-file") {
			return withForm(cmd, args)
		}

		return addEventsFromFile(ctx, cmd, app, args)
	}

	return cmd
}

func eventAddDefinition() definition {
	return definition{fields: []field{
		inherited(timelineField()),
		{name: "text", arg: true, required: true, perEntry: true, usage: "What happened, as `text`, or - to read it from stdin", check: withoutEnv(checkTextOrStdin)},
		// Before at, so a form asks for the zone before the times read in it.
		tzField(),
		{
			name: "at", perEntry: true, usage: "When it happened, as a `time` (default: now)", check: checkAt,
			preview: func(at string, env checkEnv) string {
				raw := &at
				if at == "" {
					raw = nil
				}
				r, err := resolveAt(raw, env.now, env.zone.loc)
				return timePreview(r, err, env.zone)
			},
		},
		{
			name: "ended", perEntry: true, usage: "When it ended, as a `time`, for an event that lasted",
			check: func(ended string, env checkEnv) error {
				_, err := resolveEnded(ended, env, addedEndedHint)
				return err
			},
			preview: func(ended string, env checkEnv) string {
				if ended == "" {
					return ""
				}
				r, err := resolveEnded(ended, env, addedEndedHint)
				return timePreview(r, err, env.zone)
			},
		},
		{name: "approximate", typ: boolField, perEntry: true, usage: "Mark the time as a guess, as a ~ or around prefix does"},
		{name: "source", usage: "System, service or person it came from, as a `source`, created on first use", check: withoutEnv(checkSource)},
		{name: "tag", typ: stringsField, usage: "Keyword such as phase:detect, as a `tag`; repeat for more", check: withoutEnv(checkTag)},
		{name: "url", perEntry: true, usage: "Permalink to the log line or message, as a `url`", check: freeText("--url")},
		{name: "author", usage: "Who is adding it, as a `name` (default: config author, then USER)", check: freeText("--author")},
		perEntry(attachField()),
		{
			name: "id", perEntry: true, usage: "The event's id, as a lowercase UUIDv7 `uuid`",
			check: withoutEnv(checkEventID),
		},
		{name: "dry-run", typ: boolField, usage: "Show the event as it would be added, and add nothing"},
	}}
}

const addedEndedHint = "~ and around mark --at only; pass --approximate to mark the event."

// eventAdded is the event add stored or, with --dry-run, would store, in its timeline, or the one
// already stored under --id.
type eventAdded struct {
	event    timeline.Event
	timeline timeline.Timeline
	// when is the event's resolved time as the command prints it.
	when            string
	attach          attachResult
	dryRun, existed bool
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
	req, err := readAttachRequest(in, app.ReadFile)
	if err != nil {
		return eventAdded{}, err
	}
	draft, err := draftEvent(in, app, cfg, env)
	if err != nil {
		return eventAdded{}, err
	}
	req, err = req.staged(app.NewID, now)
	if err != nil {
		return eventAdded{}, err
	}

	owner := timeline.AttachmentOwner{EventID: &draft.event.ID}
	built, inserted, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(req.dryRun), Attach: req.toAdd},
		func(tx *store.Tx) (attached[eventAdded], error) {
			b := attached[eventAdded]{
				result: eventAdded{event: draft.event, when: draft.when, dryRun: req.dryRun}, owner: owner,
			}
			t, err := selectOne(ctx, tx, timelineInputOf(in), app.LookupEnv)
			if err != nil {
				return b, err
			}
			b.result.timeline = t
			b.result.event.TimelineID = t.ID
			if in.given("id") {
				b.result.event, b.result.existed, err = storedSameEvent(ctx, tx, b.result.event, req.files)
				if err != nil {
					return b, err
				}
			}
			if b.result.existed {
				b.stored, err = tx.ListAttachments(ctx, owner)
				return b, err
			}
			b.result.event, err = tx.AddEvent(ctx, b.result.event, draft.newSourceID)
			if err != nil {
				return b, err
			}
			err = b.attachIn(ctx, tx, owner)
			return b, err
		})
	added := built.result
	if err != nil {
		return added, err
	}
	if req.dryRun && !added.existed {
		added.event.Seq = 0 // A dry run claims no seq.
	}
	if added.existed {
		// Nothing is attached, so result lists the stored attachments and reports none as added.
		req = attachRequest{}
	}
	added.attach = built.attachResult(req, inserted, cfg.DBPath)

	return added, nil
}

// storedSameEvent returns the event stored under e's id and true when adding e, with files
// attached, would change nothing; e and false when no event has that id; and id_conflict when
// adding e would change the stored event.
func storedSameEvent(ctx context.Context, s records, e timeline.Event, files []attachFile) (timeline.Event, bool, error) {
	stored, err := s.Event(ctx, e.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return e, false, nil
	case err != nil:
		return e, false, err
	}
	attachments, err := s.ListAttachments(ctx, timeline.AttachmentOwner{EventID: &stored.ID})
	if err != nil {
		return e, false, err
	}

	// The store set the stored event's times; a re-add brings its own clock's.
	keys := eventDiff(stored, e, keyCreatedAt, keyUpdatedAt)
	if !slices.Equal(sortedSHA256s(attachments), fileSHA256s(files)) {
		keys = append(keys, "attachments")
	}
	if len(keys) > 0 {
		return e, false, idConflictError(e.ID, "it is stored with different "+strings.Join(keys, ", "), nil)
	}

	return stored, true, nil
}

func sortedSHA256s(attachments []timeline.Attachment) []string {
	sums := make([]string, 0, len(attachments))
	for _, a := range attachments {
		sums = append(sums, a.SHA256)
	}
	slices.Sort(sums)

	return sums
}

func fileSHA256s(files []attachFile) []string {
	sums := make([]string, 0, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.content)
		sums = append(sums, hex.EncodeToString(sum[:]))
	}
	slices.Sort(sums)

	return sums
}

func idConflictError(id, why string, err error) *Error {
	return &Error{
		Kind: KindIDConflict, Message: fmt.Sprintf("event %s already exists: %s, so nothing was added", id, why),
		Hint: fmt.Sprintf("Pass a new --id to add another event, or change this one with 'coroner event edit %s'.", id), Err: err,
	}
}

// checkEventID rejects an id that is not a UUIDv7 written as lowercase hex with hyphens, the ids
// coroner makes.
func checkEventID(id string) error {
	parsed, err := uuid.Parse(id)
	if err == nil && parsed.Version() == 7 && parsed.String() == id {
		return nil
	}

	return &Error{
		Kind: KindUsage, Message: fmt.Sprintf("--id %q is not a lowercase UUIDv7", id),
		Hint: "Pass a version 7 UUID in lowercase, such as 01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2.", Err: err,
	}
}

func (a eventAdded) render(o output) error {
	if a.existed {
		err := printEvent(o, a.event, a.attach.objects)
		if err == nil && !a.dryRun {
			err = o.printID(a.event.ID)
		}
		if err != nil {
			return err
		}

		return o.notify("Event %s is already #%d in timeline `%s`, so nothing was added", a.event.ID, a.event.Seq, a.timeline.Name)
	}
	if a.dryRun {
		err := previewEvent(o, a.timeline, a.event, a.attach.objects, a.when)
		if err == nil {
			err = a.attach.notify(o)
		}

		return err
	}

	err := printEvent(o, a.event, a.attach.objects)
	if err == nil {
		err = o.printID(a.event.ID)
	}
	if err == nil {
		err = a.attach.notify(o)
	}
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
// keeping --id's event id when given, rejecting anything the store would. It reads stdin last,
// so a bad field never waits on it, and draws ids only for a valid event, so a rejected event
// leaves the id sequence, which scripts number from, where it was.
func draftEvent(in fieldValues, app App, cfg config.Config, env checkEnv) (eventDraft, error) {
	fields, err := readEventFields(in, cfg.Author, app.LookupEnv)
	if err != nil {
		return eventDraft{}, err
	}
	times, err := resolveEventTimes(in, env)
	if err != nil {
		return eventDraft{}, err
	}
	text, err := eventText(in.values("text"), app.In, app.InIsTTY)
	if err != nil {
		return eventDraft{}, err
	}

	e := timeline.Event{
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
	err = recordError(e.Validate(), "Run 'coroner event add --help' for what each flag takes.")
	if err != nil {
		return eventDraft{}, err
	}
	e.ID, err = newEventID(in.value("id"), app.NewID)
	if err != nil {
		return eventDraft{}, err
	}
	sourceID, err := newSourceID(fields.source, app.NewID)
	if err != nil {
		return eventDraft{}, err
	}

	return eventDraft{event: e, newSourceID: sourceID, when: times.when}, nil
}

// newEventID returns id when it was given, and a new id otherwise.
func newEventID(id *string, newID func() (uuid.UUID, error)) (string, error) {
	if id != nil {
		return *id, nil
	}

	generated, err := newID()
	return generated.String(), err
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

// checkTextOrStdin checks event text as typed, where - stands for text read from stdin later.
func checkTextOrStdin(text string) error {
	if text == "-" {
		return nil
	}

	_, err := checkText(text)
	return err
}

func checkText(text string) (string, error) {
	err := timeline.ValidateText("text", text)
	p, ok := firstProblem(err)
	switch {
	case !ok:
		return text, nil
	case p.Rule == timeline.RuleBlank:
		return "", &Error{Kind: KindUsage, Message: "event text must not be blank", Hint: "Pass non-blank text."}
	case p.Rule == timeline.RuleNotUTF8:
		return "", &Error{
			Kind: KindUsage, Message: "event text must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8",
		}
	}

	return "", recordError(err, "")
}

// eventFields are the source, tags, URL and author an event's fields give it.
type eventFields struct {
	source, url, author *string
	tags                []string
}

// readEventFields returns the event fields in gives, with the author defaulting to
// configuredAuthor, then USER.
func readEventFields(in fieldValues, configuredAuthor string, lookupEnv func(string) (string, bool)) (eventFields, error) {
	author, err := eventAuthor(in.value("author"), configuredAuthor, lookupEnv)

	return eventFields{
		source: in.value("source"),
		tags:   appendUnique(nil, in.values("tag")...),
		url:    in.value("url"),
		author: author,
	}, err
}

func checkNotBlank(flag string, value *string) error {
	if value == nil || strings.TrimSpace(*value) != "" {
		return nil
	}

	return &Error{Kind: KindUsage, Message: flag + " must not be blank", Hint: "Leave " + flag + " out instead."}
}

// checkUTF8 returns usage when value of flag is not UTF-8.
func checkUTF8(flag, value string) error {
	if timeline.ValidateUTF8(flag, value) == nil {
		return nil
	}

	return &Error{Kind: KindUsage, Message: flag + " must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8"}
}

// utf8Only returns a field check that rejects a value of flag that is not UTF-8.
func utf8Only(flag string) func(string, checkEnv) error {
	return withoutEnv(func(value string) error {
		return checkUTF8(flag, value)
	})
}

// freeText returns a field check that rejects a blank value of flag, or one that is not UTF-8.
func freeText(flag string) func(string, checkEnv) error {
	return withoutEnv(func(value string) error {
		err := checkNotBlank(flag, &value)
		if err != nil {
			return err
		}

		return checkUTF8(flag, value)
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

// eventAuthor returns flag, else configured, else USER, or nil when all are empty, rejecting a
// defaulted author that is not UTF-8.
func eventAuthor(flag *string, configured string, lookupEnv func(string) (string, bool)) (*string, error) {
	if flag != nil {
		return flag, nil
	}

	user, _ := lookupEnv("USER")
	author := cmp.Or(configured, user)
	if timeline.ValidateUTF8("author", author) != nil {
		return nil, &Error{
			Kind:    KindUsage,
			Message: "the default author, from the config file's author or USER, must be UTF-8",
			Hint:    "Pass --author, or fix the config author or USER.",
		}
	}

	var defaulted *string
	if author != "" {
		defaulted = &author
	}

	return defaulted, nil
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
// rejecting an --ended that is approximate.
func resolveEventTimes(in fieldValues, env checkEnv) (eventTimes, error) {
	raw := in.value("at")
	at, err := resolveAt(raw, env.now, env.zone.loc)
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
	endedWhen := formatResolved(ended, env.zone)
	err = timeline.ValidateEndedAt(&ended.Instant, at.Instant)
	if p, ok := firstProblem(err); ok && p.Rule == timeline.RuleEndedBeforeOccurred {
		return eventTimes{}, &Error{
			Kind:    kindOf(p.Rule),
			Message: fmt.Sprintf("--ended %s is before --at %s", endedWhen, times.when),
			Hint:    "Pass an --ended time no earlier than --at.",
			Err:     err,
		}
	}
	times.ended = &ended.Instant
	times.when += ", ended " + endedWhen

	return times, nil
}

// printEvent prints e with attachments when o asks for JSON; text output leaves it to the
// command's message.
func printEvent(o output, e timeline.Event, attachments []attachmentJSON) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newEventJSON(e, attachments))
}

// previewEvent prints e as it would be added to t at when: its JSON object with attachments, or
// a description.
func previewEvent(o output, t timeline.Timeline, e timeline.Event, attachments []attachmentJSON, when string) error {
	if o.format == formatJSON {
		return o.json(newEventJSON(e, attachments))
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
