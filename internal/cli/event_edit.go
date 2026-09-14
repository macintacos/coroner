package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

// clearKeyword is the value that clears an optional field (A-9); keep in sync with
// timeline.ValidateSourceName and the source.name CHECK, which reject it as a source.
const clearKeyword = "none"

// eventEditFlags are the flags that change a field; edit needs at least one.
func eventEditFlags() []string {
	return []string{"text", "at", "ended", "approximate", "source", "add-tag", "remove-tag", "url", "author", "attach"}
}

func newEventEditCommand(ctx context.Context, app App) *cobra.Command {
	def := eventEditDefinition()
	cmd := &cobra.Command{
		Use:   "edit <event>",
		Short: "Change an event's text, time, source, tags or other fields",
		Long: "Change the fields of an event. --at and --ended are read as 'coroner event add' reads them,\n" +
			"and the times they resolve to are printed on stderr. Tags are removed before they are\n" +
			"added; adding a tag it has, or removing one it lacks, changes nothing. Pass none to\n" +
			"--source, --url, --author or --ended to clear it. Pass --text - to read the text from\n" +
			"stdin, which is never read otherwise. --attach adds a file to the event's attachments;\n" +
			"'coroner attachment delete' removes one.\n\n" + eventRefHelp,
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event edit 12 --add-tag root-cause --remove-tag election --dry-run\n" +
			"  $ coroner event edit 12 --attach ./grafana-p99.png\n" +
			"  $ coroner event edit mongo-outage-2026-09-11#12 --at \"2026-09-11T03:14:07.412Z\" --source none",
		RunE: writeRunE(ctx, app, def, editEvent),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func eventEditDefinition() definition {
	return definition{fields: []field{
		inherited(timelineField()),
		{name: "event", arg: true, usage: "Event to edit, as a `ref`: seq, timeline#seq or id", check: withoutEnv(checkEventRef)},
		{name: "text", usage: "New `text`, or - to read it from stdin", check: withoutEnv(checkTextOrStdin)},
		{name: "at", usage: "When it happened, as a `time`", check: checkAt},
		{
			name: "ended", usage: "When it ended, as a `time`, or none",
			check: func(ended string, env checkEnv) error {
				if ended == clearKeyword {
					return nil
				}
				_, err := resolveEnded(ended, env, editedEndedHint)
				return err
			},
		},
		{name: "approximate", typ: boolField, usage: "Mark the time as a guess; --approximate=false unmarks it"},
		{name: "source", usage: "System, service or person it came from, as a `source`, or none", check: withoutEnv(checkEditedSource)},
		{name: "add-tag", typ: stringsField, usage: "Add a `tag`; repeat for more", check: withoutEnv(checkTag)},
		{name: "remove-tag", typ: stringsField, usage: "Remove a `tag`; repeat for more"},
		{name: "url", usage: "Permalink to the log line or message, as a `url`, or none", check: clearableNotBlank("--url")},
		{name: "author", usage: "Who added it, as a `name`, or none", check: clearableNotBlank("--author")},
		attachField(),
		{name: "dry-run", typ: boolField, usage: "Show the edited event, and change nothing"},
		tzField(),
	}}
}

const editedEndedHint = "~ and around mark --at only; pass --approximate."

// checkEditedSource accepts none, which clears the source, and otherwise a source name.
func checkEditedSource(source string) error {
	if source == clearKeyword {
		return nil
	}
	err := checkNotBlank("--source", &source)
	if err != nil {
		return err
	}

	return checkSource(source)
}

// clearableNotBlank returns a field check that accepts none, which clears the field, and
// otherwise rejects a blank value of flag.
func clearableNotBlank(flag string) func(string, checkEnv) error {
	return withoutEnv(func(value string) error {
		if value == clearKeyword {
			return nil
		}

		return checkNotBlank(flag, &value)
	})
}

// eventEdited is the event as edit stored it or, with --dry-run, would store it, with the zone
// its times print in.
type eventEdited struct {
	event    timeline.Event
	timeline timeline.Timeline
	zone     zone
	// when is the edit's resolved times as the command echoes them, or "" when it sets none.
	when   string
	attach attachResult
	dryRun bool
}

func editEvent(ctx context.Context, app App, cfg config.Config, in fieldValues) (eventEdited, error) {
	now := app.Now()
	z, err := timeZone(in.value("tz"), cfg.DefaultTimezone, now.Location())
	if err != nil {
		return eventEdited{}, err
	}
	env := checkEnv{now: now, zone: z}
	err = eventEditDefinition().check(in, env)
	if err != nil {
		return eventEdited{}, err
	}
	event := in.value("event")
	if event == nil {
		return eventEdited{}, missingValue("event edit", "the event's ref")
	}
	ref, err := parseEventRef(*event)
	if err != nil {
		return eventEdited{}, err
	}
	req, err := readAttachRequest(in, app.ReadFile)
	if err != nil {
		return eventEdited{}, err
	}
	edit, err := readEventEdit(in, app, env)
	if err != nil {
		return eventEdited{}, err
	}
	req, err = req.staged(app.NewID, now)
	if err != nil {
		return eventEdited{}, err
	}

	edited := eventEdited{zone: z, when: edit.when, dryRun: req.dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		// ponytail: find and update are separate transactions, so a concurrent edit of this
		// event is overwritten; apply the edit inside one store write if that matters.
		e, t, err := findEvent(ctx, s, ref, timelineSelection{flag: in.value("timeline"), lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		e, err = edit.apply(e, z)
		if err != nil {
			return err
		}
		edited.event, edited.timeline = e, t
		if !req.dryRun {
			err = s.UpdateEvent(ctx, e, edit.newSourceID, req.add...)
			if errors.Is(err, store.ErrNotFound) {
				return eventNotFoundError("another coroner deleted the event first", err)
			}
			if err != nil {
				return err
			}
		}

		edited.attach, err = req.result(ctx, s, timeline.AttachmentOwner{EventID: &e.ID}, cfg.DBPath)
		return err
	})

	return edited, err
}

func (e eventEdited) render(o output) error {
	if e.dryRun {
		err := previewEventEdit(o, e.zone, e.event, e.attach.objects, e.timeline)
		if err == nil {
			err = e.attach.notify(o)
		}
		if err != nil {
			return err
		}

		return o.notify("Would edit event #%d in timeline `%s`%s", e.event.Seq, e.timeline.Name, e.when)
	}

	err := printEvent(o, e.event, e.attach.objects)
	if err == nil {
		err = e.attach.notify(o)
	}
	if err != nil {
		return err
	}

	return o.notify("Edited event #%d in timeline `%s`%s", e.event.Seq, e.timeline.Name, e.when)
}

// fieldEdit is an optional field's new value: set when its field was given, and a nil value
// when it said none.
type fieldEdit struct {
	set   bool
	value *string
}

func (f fieldEdit) apply(current *string) *string {
	if f.set {
		return f.value
	}

	return current
}

// eventEdit is the change edit's fields ask for, read and resolved before the store opens.
type eventEdit struct {
	editFields
	editTimes

	text *string
	now  time.Time
	// newSourceID is the id the event's source gets when no source has that name yet.
	newSourceID string
}

// editFields are the source, URL, author, approximate mark and tags an edit sets.
type editFields struct {
	source, url, author fieldEdit
	approximate         *bool
	addTags, removeTags []string
}

// editTimes are an edit's resolved --at and --ended.
type editTimes struct {
	at         *timeparse.Resolved
	raw        *string
	ended      *timeparse.Resolved
	clearEnded bool
	// when is at and ended as the command echoes them, or "" when neither was given.
	when string
}

// readEventEdit returns the edit in asks for, with times read as of env. It reads stdin last, so
// a bad field never waits on it.
func readEventEdit(in fieldValues, app App, env checkEnv) (eventEdit, error) {
	if !slices.ContainsFunc(eventEditFlags(), in.given) {
		return eventEdit{}, &Error{
			Kind:    KindUsage,
			Message: "nothing to edit",
			Hint:    "Pass --" + strings.Join(eventEditFlags(), ", --") + ".",
		}
	}

	edit := eventEdit{now: env.now, editFields: readEditFields(in)}
	var err error
	edit.editTimes, err = readEditTimes(in, env)
	if err != nil {
		return edit, err
	}
	// The store upserts the edited event's source, even one --source left unchanged, so it
	// always needs an id to create one with.
	sourceID, err := app.NewID()
	if err != nil {
		return edit, err
	}
	edit.newSourceID = sourceID.String()
	if text := in.value("text"); text != nil {
		value, err := eventText([]string{*text}, app.In, app.InIsTTY)
		edit.text = &value
		return edit, err
	}

	return edit, nil
}

// readEditFields reads the source, URL, author, approximate mark and tags.
func readEditFields(in fieldValues) editFields {
	f := editFields{
		source: readFieldEdit(in, "source"), url: readFieldEdit(in, "url"), author: readFieldEdit(in, "author"),
		addTags: appendUnique(nil, in.values("add-tag")...), removeTags: in.values("remove-tag"),
	}
	if approximate, given := in.boolean("approximate"); given {
		f.approximate = &approximate
	}

	return f
}

// readFieldEdit reads the optional field name sets, or clears with none.
func readFieldEdit(in fieldValues, name string) fieldEdit {
	value := in.value(name)
	switch {
	case value == nil:
		return fieldEdit{}
	case *value == clearKeyword:
		return fieldEdit{set: true}
	}

	return fieldEdit{set: true, value: value}
}

// readEditTimes resolves --at and --ended as of env, rejecting an approximate --ended.
func readEditTimes(in fieldValues, env checkEnv) (editTimes, error) {
	var times editTimes
	var echoes []string
	if raw := in.value("at"); raw != nil {
		at, err := resolveTime("--at", raw, env.now, env.zone.loc)
		if err != nil {
			return times, err
		}
		times.at, times.raw = &at, raw
		echoes = append(echoes, "at "+formatResolved(at, env.zone))
	}

	ended := in.value("ended")
	times.clearEnded = ended != nil && *ended == clearKeyword
	if ended != nil && !times.clearEnded {
		r, err := resolveEnded(*ended, env, editedEndedHint)
		if err != nil {
			return times, err
		}
		times.ended = &r
		echoes = append(echoes, "ended "+formatResolved(r, env.zone))
	}

	if len(echoes) > 0 {
		times.when = " " + strings.Join(echoes, ", ")
	}

	return times, nil
}

// apply returns ev with the edit made and updated at set, rejecting a result the store would,
// with times in z.
func (e eventEdit) apply(ev timeline.Event, z zone) (timeline.Event, error) {
	if e.text != nil {
		ev.Text = *e.text
	}
	if e.at != nil {
		ev.OccurredAt, ev.OccurredOffset, ev.Precision = e.at.Instant, e.at.Offset, e.at.Precision
		ev.Approximate, ev.OccurredRaw = e.at.Approximate, e.raw
	}
	if e.approximate != nil {
		ev.Approximate = *e.approximate
	}
	switch {
	case e.clearEnded:
		ev.EndedAt = nil
	case e.ended != nil:
		ev.EndedAt = &e.ended.Instant
	}
	ev.Source, ev.URL, ev.Author = e.source.apply(ev.Source), e.url.apply(ev.URL), e.author.apply(ev.Author)
	ev.Tags = appendUnique(slices.DeleteFunc(slices.Clone(ev.Tags), func(tag string) bool {
		return slices.Contains(e.removeTags, tag)
	}), e.addTags...)
	slices.Sort(ev.Tags)
	ev.UpdatedAt = e.now

	return ev, checkEditedEvent(ev, z)
}

// checkEditedEvent returns an ended at before occurred at as invalid_time, naming both in z, and
// any other rule ev breaks as usage.
func checkEditedEvent(ev timeline.Event, z zone) error {
	if ev.EndedAt != nil && ev.EndedAt.Before(ev.OccurredAt) {
		return &Error{
			Kind: KindInvalidTime,
			Message: fmt.Sprintf("the event would end at %s, before it occurred at %s",
				ev.EndedAt.In(z.loc).Format(time.RFC3339Nano), occurredText(ev, z)),
			Hint: "Pass an --ended time no earlier than its occurred at, or --ended none.",
		}
	}

	err := ev.Validate()
	if err != nil {
		return &Error{Kind: KindUsage, Message: err.Error(), Hint: "Run 'coroner event edit --help' for what each flag takes.", Err: err}
	}

	return nil
}

// previewEventEdit prints ev as the edit would store it: its JSON object with attachments, or
// its summary.
func previewEventEdit(o output, z zone, ev timeline.Event, attachments []attachmentJSON, t timeline.Timeline) error {
	if o.format == formatJSON {
		return o.json(newEventJSON(ev, attachments))
	}

	_, err := io.WriteString(o.out, eventSummary(ev, t, z))
	return err
}
