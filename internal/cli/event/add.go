package event

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/merge"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

const missingTextHint = "Pass the text, or - to read it from stdin:\n" +
	"  $ coroner event add \"Primary stepped down\" --at 2026-09-11T03:14:07Z\n" +
	"  $ pbpaste | coroner event add - --source slack\n" +
	"Run 'coroner event add --help' for more."

func newEventAddCommand(ctx context.Context, app clikit.App, newTimeline clikit.TimelineAsk) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add {<text> | - | --from-file {<file> | -}}",
		Short: "Add an event to the current timeline",
		Long: "Add an event to the current timeline, and print the time it resolved to.\n\n" +
			"A time with no offset is read in --tz, then the config file's default_timezone, then UTC.\n" +
			"RFC 3339 is stable and natural language experimental, so scripts send RFC 3339.\n" +
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
			"On a terminal, with no text or with --interactive, a form asks for the text, time, source,\n" +
			"tags and files, and keeps the other flags under More options, which opens by itself when one\n" +
			"of them holds a value. It suggests sources and tags, the ones on the timeline's latest\n" +
			"events first, previews the time as you type it, and prints the command each entry equals.\n" +
			"It never asks for --id, which applies to the first entry only. Its frame names the timeline,\n" +
			"which ctrl+t switches or creates, and counts the events added, which it lists when you\n" +
			"finish; esc on an entry finishes at once. Under --json only --interactive opens it, and\n" +
			"--no-input never does.",
		Args:        clikit.EventTextInput().Args,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
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

	AddForm(newTimeline).Attach(ctx, cmd, app, clikit.TerminalForms(app, "📝 New event"))
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

// AddDefinition declares `event add`'s flags, argument and their checks.
func AddDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		clikit.Inherited(clikit.TimelineField()),
		{Name: "text", Arg: true, Required: true, PerEntry: true, Title: "What happened", Usage: "What happened, as `text`, or - to read it from stdin", Check: clikit.WithoutEnv(clikit.EventTextInput().CheckOrStdin)},
		clikit.More(clikit.TzField()),
		{
			Name: "at", PerEntry: true, Title: "When", Usage: "When it happened, as a `time` (default: now)", Check: clikit.CheckAt,
			Preview: func(at string, env clikit.CheckEnv) string {
				raw := &at
				if at == "" {
					raw = nil
				}
				r, err := clikit.ResolveAt(raw, env.Now, env.Zone.Loc)
				return clikit.TimePreview(r, err, env.Zone)
			},
		},
		{
			Name: "ended", PerEntry: true, More: true, Usage: "When it ended, as a `time`, for an event that lasted",
			Check: func(ended string, env clikit.CheckEnv) error {
				_, err := clikit.ResolveEnded(ended, env, AddedEndedHint)
				return err
			},
			Preview: func(ended string, env clikit.CheckEnv) string {
				if ended == "" {
					return ""
				}
				r, err := clikit.ResolveEnded(ended, env, AddedEndedHint)
				return clikit.TimePreview(r, err, env.Zone)
			},
		},
		{Name: "approximate", Typ: clikit.BoolField, PerEntry: true, More: true, Usage: "Mark the time as a guess, as a ~ or around prefix does"},
		{Name: "source", Title: "Source", Usage: "System, service or person it came from, as a `source`, created on first use", Check: clikit.WithoutEnv(clikit.CheckSource)},
		{Name: "tag", Typ: clikit.StringsField, List: true, Title: "Tags", Usage: "Keyword such as phase:detect, as a `tag`; repeat for more", Check: clikit.WithoutEnv(clikit.CheckTag)},
		{Name: "url", PerEntry: true, More: true, Usage: "Permalink to the log line or message, as a `url`", Check: clikit.FreeText("--url")},
		{Name: "author", More: true, Usage: "Who is adding it, as a `name` (default: config author, then USER)", Check: clikit.FreeText("--author")},
		clikit.Titled(clikit.PerEntry(clikit.AttachField()), "Files"),
		{
			Name: "id", PerEntry: true, Usage: "The event's id, as a lowercase UUIDv7 `uuid`",
			Check: clikit.WithoutEnv(checkEventID),
		},
		{Name: "dry-run", Typ: clikit.BoolField, More: true, Usage: "Show the event as it would be added, and add nothing"},
	}}
}

// AddedEndedHint is the hint of an event add whose --ended is approximate.
const AddedEndedHint = "~ and around mark --at only; pass --approximate to mark the event."

// eventAdded is the event add stored or, with --dry-run, would store, in its timeline, or the one
// already stored under --id.
type eventAdded struct {
	event    timeline.Event
	timeline timeline.Timeline
	// when is the event's resolved time as the command prints it.
	when            string
	attach          clikit.AttachResult
	dryRun, existed bool
}

func (a eventAdded) LogChanges(ctx context.Context, logger *slog.Logger) {
	if a.existed {
		return
	}
	logger = clikit.WithDryRun(logger, a.dryRun)
	logger.InfoContext(ctx, "event added", "event", a.event.ID, "timeline_id", a.timeline.ID)
	a.attach.LogAdded(ctx, logger)
}

func addEvent(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (eventAdded, error) {
	now := app.Now()
	z, err := clikit.TimeZone(in.Value("tz"), cfg.DefaultTimezone, now.Location())
	if err != nil {
		return eventAdded{}, err
	}
	env := clikit.CheckEnv{Now: now, Zone: z}
	err = AddDefinition().Check(in, env)
	if err != nil {
		return eventAdded{}, err
	}
	req, err := clikit.ReadAttachRequest(in, app.ReadFile)
	if err != nil {
		return eventAdded{}, err
	}
	draft, err := draftEvent(in, app, cfg, env)
	if err != nil {
		return eventAdded{}, err
	}
	req, err = req.Staged(app.NewID, now)
	if err != nil {
		return eventAdded{}, err
	}

	owner := timeline.AttachmentOwner{EventID: &draft.event.ID}
	built, inserted, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(req.DryRun), Attach: req.ToAdd},
		func(tx *store.Tx) (clikit.Attached[eventAdded], error) {
			b := clikit.Attached[eventAdded]{
				Result: eventAdded{event: draft.event, when: draft.when, dryRun: req.DryRun}, Owner: owner,
			}
			t, err := clikit.SelectOne(ctx, app.Logger(), tx, clikit.TimelineInputOf(in), app.LookupEnv)
			if err != nil {
				return b, err
			}
			b.Result.timeline = t
			b.Result.event.TimelineID = t.ID
			if in.Given("id") {
				b.Result.event, b.Result.existed, err = storedSameEvent(ctx, tx, b.Result.event, req.Files)
				if err != nil {
					return b, err
				}
			}
			if b.Result.existed {
				b.Stored, err = tx.ListAttachments(ctx, owner)
				return b, err
			}
			b.Result.event, err = tx.AddEvent(ctx, b.Result.event, draft.newSourceID)
			if err != nil {
				return b, err
			}
			err = b.AttachIn(ctx, tx, owner)
			return b, err
		})
	added := built.Result
	if err != nil {
		return added, err
	}
	if req.DryRun && !added.existed {
		added.event.Seq = 0 // A dry run claims no seq.
	}
	if added.existed {
		// Nothing is attached, so result lists the stored attachments and reports none as added.
		req = clikit.AttachRequest{}
	}
	added.attach = built.AttachResult(req, inserted, cfg.DBPath)

	return added, nil
}

// storedSameEvent returns the event stored under e's id and true when adding e, with files
// attached, would change nothing; e and false when no event has that id; and id_conflict when
// adding e would change the stored event.
func storedSameEvent(ctx context.Context, s clikit.Records, e timeline.Event, files []clikit.AttachFile) (timeline.Event, bool, error) {
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
	keys := merge.EventDiff(stored, e, clikit.KeyCreatedAt, clikit.KeyUpdatedAt)
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

func fileSHA256s(files []clikit.AttachFile) []string {
	sums := make([]string, 0, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.Content)
		sums = append(sums, hex.EncodeToString(sum[:]))
	}
	slices.Sort(sums)

	return sums
}

func idConflictError(id, why string, err error) *clikit.Error {
	return &clikit.Error{
		Kind: clikit.KindIDConflict, Message: fmt.Sprintf("event %s already exists: %s, so nothing was added", id, why),
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

	return &clikit.Error{
		Kind: clikit.KindUsage, Message: fmt.Sprintf("--id %q is not a lowercase UUIDv7", id),
		Hint: "Pass a version 7 UUID in lowercase, such as 01a0a07c-cc3e-764d-b6e9-6d030ebfc0b2.", Err: err,
	}
}

func (a eventAdded) Render(o clikit.Output) error {
	if a.existed {
		err := printEvent(o, a.event, a.attach.Objects)
		if err == nil && !a.dryRun {
			err = o.PrintID(a.event.ID)
		}
		if err != nil {
			return err
		}

		return o.Notify("Event %s is already #%d in timeline `%s`, so nothing was added", a.event.ID, a.event.Seq, a.timeline.Name)
	}
	if a.dryRun {
		err := previewEvent(o, a.timeline, a.event, a.attach.Objects, a.when)
		if err == nil {
			err = a.attach.Notify(o)
		}

		return err
	}

	err := printEvent(o, a.event, a.attach.Objects)
	if err == nil {
		err = o.PrintID(a.event.ID)
	}
	if err == nil {
		err = a.attach.Notify(o)
	}
	if err != nil {
		return err
	}

	return o.Notify("Added event #%d to timeline `%s` at %s", a.event.Seq, a.timeline.Name, a.when)
}

func (a eventAdded) Summary() (clikit.SummaryLine, bool) {
	cells := []string{fmt.Sprintf("#%d", a.event.Seq), a.when, clikit.PickerLabel(a.event.Text)}
	return clikit.SummaryLine{Timeline: a.timeline.Name, Cells: cells}, !a.dryRun && !a.existed
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
func draftEvent(in clikit.FieldValues, app clikit.App, cfg config.Config, env clikit.CheckEnv) (eventDraft, error) {
	fields, err := readEventFields(in, cfg.Author, app.LookupEnv)
	if err != nil {
		return eventDraft{}, err
	}
	times, err := resolveEventTimes(app.Logger(), in, env)
	if err != nil {
		return eventDraft{}, err
	}
	text, err := eventText(in.Values("text"), app.In, app.InIsTTY)
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
		CreatedAt:      env.Now,
		UpdatedAt:      env.Now,
	}
	err = clikit.RecordError(e.Validate(), "Run 'coroner event add --help' for what each flag takes.")
	if err != nil {
		return eventDraft{}, err
	}
	e.ID, err = newEventID(in.Value("id"), app.NewID)
	if err != nil {
		return eventDraft{}, err
	}
	sourceID, err := clikit.NewSourceID(fields.source, app.NewID)
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

// eventText returns the text args give, reading in when that is -.
func eventText(args []string, in io.Reader, inIsTTY bool) (string, error) {
	if len(args) == 0 {
		return "", &clikit.Error{Kind: clikit.KindUsage, Message: "event add needs the event's text", Hint: missingTextHint}
	}

	return clikit.EventTextInput().Read(args[0], in, inIsTTY)
}

// eventFields are the source, tags, URL and author an event's fields give it.
type eventFields struct {
	source, url, author *string
	tags                []string
}

// readEventFields returns the event fields in gives, with the author defaulting to
// configuredAuthor, then USER.
func readEventFields(in clikit.FieldValues, configuredAuthor string, lookupEnv func(string) (string, bool)) (eventFields, error) {
	author, err := clikit.DefaultAuthor(in.Value("author"), configuredAuthor, lookupEnv)

	return eventFields{
		source: in.Value("source"),
		tags:   clikit.AppendUnique(nil, in.Values("tag")...),
		url:    in.Value("url"),
		author: author,
	}, err
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
func resolveEventTimes(logger *slog.Logger, in clikit.FieldValues, env clikit.CheckEnv) (eventTimes, error) {
	raw := in.Value("at")
	at, err := clikit.ResolveAt(raw, env.Now, env.Zone.Loc)
	if err != nil {
		return eventTimes{}, err
	}
	if raw != nil {
		clikit.LogTypedTime(logger, clikit.TypedTime{Flag: "at", Input: *raw, Zone: env.Zone, Resolved: at})
	}
	approximate, _ := in.Boolean("approximate")
	at.Approximate = at.Approximate || approximate
	times := eventTimes{at: at, raw: raw, when: clikit.FormatResolved(at, env.Zone)}

	endedInput := in.Value("ended")
	if endedInput == nil {
		return times, nil
	}
	ended, err := clikit.ResolveEnded(*endedInput, env, AddedEndedHint)
	if err != nil {
		return eventTimes{}, err
	}
	clikit.LogTypedTime(logger, clikit.TypedTime{Flag: "ended", Input: *endedInput, Zone: env.Zone, Resolved: ended})
	endedWhen := clikit.FormatResolved(ended, env.Zone)
	err = timeline.ValidateEndedAt(&ended.Instant, at.Instant)
	if p, ok := clikit.FirstProblem(err); ok && p.Rule == timeline.RuleEndedBeforeOccurred {
		return eventTimes{}, &clikit.Error{
			Kind:    clikit.KindOf(p.Rule),
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
func printEvent(o clikit.Output, e timeline.Event, attachments []clikit.AttachmentJSON) error {
	if o.Format != clikit.FormatJSON {
		return nil
	}

	return o.JSON(NewJSON(e, attachments))
}

// previewEvent prints e as it would be added to t at when: its JSON object with attachments, or
// a description.
func previewEvent(o clikit.Output, t timeline.Timeline, e timeline.Event, attachments []clikit.AttachmentJSON, when string) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(NewJSON(e, attachments))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Would add to timeline `%s` at %s\n", t.Name, when)
	clikit.WriteField(&b, "Precision:", string(e.Precision))
	clikit.WriteField(&b, "Source:", clikit.OrDash(e.Source))
	clikit.WriteField(&b, "Tags:", cmp.Or(strings.Join(e.Tags, ", "), "-"))
	clikit.WriteField(&b, "URL:", clikit.OrDash(e.URL))
	clikit.WriteField(&b, "Author:", clikit.OrDash(e.Author))
	clikit.WriteField(&b, "Text:", e.Text)

	_, err := io.WriteString(o.Out, b.String())
	return err
}

// AddForm returns `event add`'s form, whose timeline picker creates a timeline through newTimeline.
func AddForm(newTimeline clikit.TimelineAsk) clikit.AddForm[eventAdded] {
	return clikit.AddForm[eventAdded]{
		Path: "event add", Def: AddDefinition(), Run: addEvent,
		Choices: eventChoices, NewTimeline: newTimeline,
	}
}

// eventChoices suggests sources as SourceChoices does, and t's tags, ordered by their latest event's
// occurred at, latest first, then every other tag there is.
func eventChoices(ctx context.Context, s *store.Store, t timeline.Timeline, _ clikit.Zone) (clikit.FormChoices, error) {
	choices, err := clikit.SourceChoices(ctx, s, t)
	if err != nil {
		return clikit.FormChoices{}, err
	}
	timelineTags, err := s.TagUsages(ctx, t.ID)
	if err != nil {
		return clikit.FormChoices{}, err
	}
	everyTag, err := s.TagUsages(ctx)
	if err != nil {
		return clikit.FormChoices{}, err
	}
	choices.Suggestions["tag"] = clikit.AppendUnique(tagNames(timelineTags), tagNames(everyTag)...)

	return choices, nil
}

func tagNames(usages []store.TagUsage) []string {
	names := make([]string, 0, len(usages))
	for _, usage := range usages {
		names = append(names, usage.Name)
	}
	return names
}
