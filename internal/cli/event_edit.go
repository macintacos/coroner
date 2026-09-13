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
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

// clearKeyword is the value that clears an optional field (A-9); keep in sync with
// timeline.ValidateSourceName and the source.name CHECK, which reject it as a source.
const clearKeyword = "none"

// eventEditFlags are the flags that change a field; edit needs at least one.
func eventEditFlags() []string {
	return []string{"text", "at", "ended", "approximate", "source", "add-tag", "remove-tag", "url", "author"}
}

func newEventEditCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <event>",
		Short: "Change an event's text, time, source, tags or other fields",
		Long: "Change the fields of an event. --at and --ended are read as 'coroner event add' reads them,\n" +
			"and the times they resolve to are printed on stderr. Tags are removed before they are\n" +
			"added; adding a tag it has, or removing one it lacks, changes nothing. Pass none to\n" +
			"--source, --url, --author or --ended to clear it. Pass --text - to read the text from\n" +
			"stdin, which is never read otherwise.\n\n" + eventRefHelp,
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner event edit 12 --add-tag root-cause --remove-tag election --dry-run\n" +
			"  $ coroner event edit mongo-outage-2026-09-11#12 --at \"2026-09-11T03:14:07.412Z\" --source none",
		RunE: func(cmd *cobra.Command, args []string) error {
			return editEvent(ctx, cmd, app, args[0])
		},
	}

	flags := cmd.Flags()
	flags.String("text", "", "New `text`, or - to read it from stdin")
	flags.String("at", "", "When it happened, as a `time`")
	flags.String("ended", "", "When it ended, as a `time`, or none")
	flags.Bool("approximate", false, "Mark the time as a guess; --approximate=false unmarks it")
	flags.String("source", "", "System, service or person it came from, as a `source`, or none")
	flags.StringArray("add-tag", nil, "Add a `tag`; repeat for more")
	flags.StringArray("remove-tag", nil, "Remove a `tag`; repeat for more")
	flags.String("url", "", "Permalink to the log line or message, as a `url`, or none")
	flags.String("author", "", "Who added it, as a `name`, or none")
	flags.Bool("dry-run", false, "Show the edited event, and change nothing")

	return cmd
}

func editEvent(ctx context.Context, cmd *cobra.Command, app App, ref string) error {
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
	edit, err := readEventEdit(cmd.Flags(), app, z)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		// ponytail: find and update are separate transactions, so a concurrent edit of this
		// event is overwritten; apply the edit inside one store write if that matters.
		e, t, err := findEvent(ctx, s, parsed, timelineSelection{flag: flag, lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		e, err = edit.apply(e, z)
		if err != nil {
			return err
		}
		if dryRun {
			err = previewEventEdit(o, z, e, t)
			if err != nil {
				return err
			}

			return o.notify("Would edit event #%d in timeline `%s`%s", e.Seq, t.Name, edit.when)
		}

		err = s.UpdateEvent(ctx, e, edit.newSourceID)
		if errors.Is(err, store.ErrNotFound) {
			return eventNotFoundError("another coroner deleted the event first", err)
		}
		if err != nil {
			return err
		}
		err = printEvent(o, e)
		if err != nil {
			return err
		}

		return o.notify("Edited event #%d in timeline `%s`%s", e.Seq, t.Name, edit.when)
	})
}

// fieldEdit is an optional field's new value: set when its flag was given, and a nil value
// when the flag said none.
type fieldEdit struct {
	set   bool
	value *string
}

func (f fieldEdit) apply(value *string) *string {
	if f.set {
		return f.value
	}

	return value
}

// eventEdit is the change edit's flags ask for, read and resolved before the store opens.
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

// readEventEdit returns the edit the flags ask for, with times read in z. It reads stdin
// last, so a bad flag never waits on it.
func readEventEdit(flags *pflag.FlagSet, app App, z zone) (eventEdit, error) {
	if !slices.ContainsFunc(eventEditFlags(), flags.Changed) {
		return eventEdit{}, &Error{
			Kind:    KindUsage,
			Message: "nothing to edit",
			Hint:    "Pass --" + strings.Join(eventEditFlags(), ", --") + ".",
		}
	}

	edit := eventEdit{now: app.Now()}
	var err error
	edit.editFields, err = readEditFields(flags)
	if err != nil {
		return edit, err
	}
	edit.editTimes, err = readEditTimes(flags, edit.now, z)
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
	if text := changedString(flags, "text"); text != nil {
		value, err := eventText([]string{*text}, app.In, app.InIsTTY)
		edit.text = &value
		return edit, err
	}

	return edit, nil
}

// readEditFields reads the source, URL, author, approximate mark and tags, rejecting any no
// event could have.
func readEditFields(flags *pflag.FlagSet) (editFields, error) {
	f := editFields{addTags: appendUnique(nil, stringArray(flags, "add-tag")...), removeTags: stringArray(flags, "remove-tag")}
	if approximate, _ := flags.GetBool("approximate"); flags.Changed("approximate") {
		f.approximate = &approximate
	}

	var err error
	for _, field := range []struct {
		name string
		edit *fieldEdit
	}{{"source", &f.source}, {"url", &f.url}, {"author", &f.author}} {
		*field.edit, err = readFieldEdit(flags, field.name)
		if err != nil {
			return f, err
		}
	}

	return f, checkSlugs(f.source.value, f.addTags)
}

// readFieldEdit reads the optional field flag name sets, or clears with none.
func readFieldEdit(flags *pflag.FlagSet, name string) (fieldEdit, error) {
	value := changedString(flags, name)
	switch {
	case value == nil:
		return fieldEdit{}, nil
	case *value == clearKeyword:
		return fieldEdit{set: true}, nil
	}

	return fieldEdit{set: true, value: value}, checkNotBlank("--"+name, value)
}

// readEditTimes resolves --at and --ended as of now in z, rejecting an approximate --ended.
func readEditTimes(flags *pflag.FlagSet, now time.Time, z zone) (editTimes, error) {
	var t editTimes
	var echoes []string
	if raw := changedString(flags, "at"); raw != nil {
		at, err := resolveTime("--at", raw, now, z.loc)
		if err != nil {
			return t, err
		}
		t.at, t.raw = &at, raw
		echoes = append(echoes, "at "+formatResolved(at, z))
	}

	ended := changedString(flags, "ended")
	t.clearEnded = ended != nil && *ended == clearKeyword
	if ended != nil && !t.clearEnded {
		r, err := resolveTime("--ended", ended, now, z.loc)
		if err != nil {
			return t, err
		}
		if r.Approximate {
			return t, &Error{
				Kind: KindInvalidTime, Message: "--ended must not be approximate", Hint: "~ and around mark --at only; pass --approximate.",
			}
		}
		t.ended = &r
		echoes = append(echoes, "ended "+formatResolved(r, z))
	}

	if len(echoes) > 0 {
		t.when = " " + strings.Join(echoes, ", ")
	}

	return t, nil
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

// previewEventEdit prints ev as the edit would store it: its JSON object, or its summary.
func previewEventEdit(o output, z zone, ev timeline.Event, t timeline.Timeline) error {
	if o.format == formatJSON {
		return o.json(newEventJSON(ev))
	}

	_, err := io.WriteString(o.out, eventSummary(ev, t, z))
	return err
}
