package event

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

func newEventEditCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := EditDefinition()
	cmd := &cobra.Command{
		Use:   "edit <event>",
		Short: "Change an event's text, time, source, tags or other fields",
		Long: "Change the fields of an event. --at and --ended are read as 'coroner event add' reads them,\n" +
			"and the times they resolve to are printed on stderr. Tags are removed before they are\n" +
			"added; adding a tag it has, or removing one it lacks, changes nothing. Pass none to\n" +
			"--source, --url, --author or --ended to clear it. Pass --text - to read the text from\n" +
			"stdin, which is never read otherwise. --attach adds a file to the event's attachments;\n" +
			"'coroner attachment delete' removes one.\n\n" + clikit.EventRefHelp,
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner event edit 12 --add-tag root-cause --remove-tag election --dry-run\n" +
			"  $ coroner event edit 12 --attach ./grafana-p99.png\n" +
			"  $ coroner event edit mongo-outage-2026-09-11#12 --at \"2026-09-11T03:14:07.412Z\" --source none",
		RunE: clikit.WriteRunE(ctx, app, def, Edit),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// EditDefinition declares `event edit`'s flags, argument and their checks.
func EditDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		clikit.Inherited(clikit.TimelineField()),
		{Name: "event", Arg: true, Usage: "Event to edit, as a `ref`: seq, timeline#seq or id", Check: clikit.WithoutEnv(clikit.CheckEventRef)},
		{Name: "text", Usage: "New `text`, or - to read it from stdin", Check: clikit.WithoutEnv(clikit.EventTextInput().CheckOrStdin), Changes: true},
		{Name: "at", Usage: "When it happened, as a `time`", Check: clikit.CheckAt, Changes: true},
		{
			Name: "ended", Usage: "When it ended, as a `time`, or none", Changes: true,
			Check: func(ended string, env clikit.CheckEnv) error {
				if ended == clikit.ClearKeyword {
					return nil
				}
				_, err := clikit.ResolveEnded(ended, env, editedEndedHint)
				return err
			},
		},
		{Name: "approximate", Typ: clikit.BoolField, Usage: "Mark the time as a guess; --approximate=false unmarks it", Changes: true},
		{Name: "source", Usage: "System, service or person it came from, as a `source`, or none", Check: clikit.WithoutEnv(clikit.CheckEditedSource), Changes: true},
		{Name: "add-tag", Typ: clikit.StringsField, Usage: "Add a `tag`; repeat for more", Check: clikit.WithoutEnv(clikit.CheckTag), Changes: true},
		{Name: "remove-tag", Typ: clikit.StringsField, Usage: "Remove a `tag`; repeat for more", Changes: true},
		{Name: "url", Usage: "Permalink to the log line or message, as a `url`, or none", Check: clikit.ClearableFreeText("--url"), Changes: true},
		{Name: "author", Usage: "Who added it, as a `name`, or none", Check: clikit.ClearableFreeText("--author"), Changes: true},
		clikit.Changing(clikit.AttachField()),
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the edited event, and change nothing"},
		clikit.TzField(),
	}}
}

const editedEndedHint = "~ and around mark --at only; pass --approximate."

// Edited is the event as edit stored it or, with --dry-run, would store it, with the zone
// its times print in.
type Edited struct {
	event    timeline.Event
	timeline timeline.Timeline
	zone     clikit.Zone
	// when is the edit's resolved times as the command echoes them, or "" when it sets none.
	when   string
	attach clikit.AttachResult
	dryRun bool
	fields []string
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (e Edited) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, e.dryRun)
	logger.InfoContext(ctx, "event edited", "event", e.event.ID, "timeline_id", e.timeline.ID, "fields", e.fields)
	e.attach.LogAdded(ctx, logger)
}

// Edit runs `event edit` from in's values and returns what it changed, or would change under --dry-run.
func Edit(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Edited, error) {
	now := app.Now()
	z, err := clikit.TimeZone(in.Value("tz"), cfg.DefaultTimezone, now.Location())
	if err != nil {
		return Edited{}, err
	}
	env := clikit.CheckEnv{Now: now, Zone: z}
	err = EditDefinition().Check(in, env)
	if err != nil {
		return Edited{}, err
	}
	event := in.Value("event")
	if event == nil {
		return Edited{}, clikit.MissingValue("event edit", "the event's ref")
	}
	ref, err := clikit.ParseEventRef(*event)
	if err != nil {
		return Edited{}, err
	}
	req, err := clikit.ReadAttachRequest(in, app.ReadFile)
	if err != nil {
		return Edited{}, err
	}
	edit, err := readEventEdit(in, app, env)
	if err != nil {
		return Edited{}, err
	}
	req, err = req.Staged(app.NewID, now)
	if err != nil {
		return Edited{}, err
	}

	built, inserted, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(req.DryRun), Attach: req.ToAdd},
		func(tx *store.Tx) (clikit.Attached[Edited], error) {
			b := clikit.Attached[Edited]{Result: Edited{zone: z, when: edit.when, dryRun: req.DryRun}}
			e, t, err := clikit.FindEvent(ctx, app.Logger(), tx, ref, clikit.TimelineInputOf(in), app.LookupEnv)
			if err != nil {
				return b, err
			}
			before := e
			e, err = edit.apply(e, z)
			if err != nil {
				return b, err
			}
			b.Result.event, b.Result.timeline = e, t
			b.Result.fields = clikit.ChangedFields(export.NewEvent(before), export.NewEvent(e))
			err = b.AttachIn(ctx, tx, timeline.AttachmentOwner{EventID: &e.ID})
			if err != nil {
				return b, err
			}
			return b, tx.UpdateEvent(ctx, e, edit.newSourceID)
		})
	edited := built.Result
	if err != nil {
		return edited, err
	}
	edited.attach = built.AttachResult(req, inserted, cfg.DBPath)

	return edited, nil
}

// Render prints the result in o's format.
func (e Edited) Render(o clikit.Output) error {
	if e.dryRun {
		err := previewEventEdit(o, e.zone, e.event, e.attach.Objects, e.timeline)
		if err == nil {
			err = e.attach.Notify(o)
		}
		if err != nil {
			return err
		}

		return o.Notify("Would edit event #%d in timeline `%s`%s", e.event.Seq, e.timeline.Name, e.when)
	}

	err := printEvent(o, e.event, e.attach.Objects)
	if err == nil {
		err = e.attach.Notify(o)
	}
	if err != nil {
		return err
	}

	return o.Notify("Edited event #%d in timeline `%s`%s", e.event.Seq, e.timeline.Name, e.when)
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
	source, url, author clikit.FieldEdit
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
func readEventEdit(in clikit.FieldValues, app clikit.App, env clikit.CheckEnv) (eventEdit, error) {
	err := EditDefinition().RequireChange(in)
	if err != nil {
		return eventEdit{}, err
	}

	edit := eventEdit{now: env.Now, editFields: readEditFields(in)}
	edit.editTimes, err = readEditTimes(app.Logger(), in, env)
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
	if text := in.Value("text"); text != nil {
		value, err := eventText([]string{*text}, app.In, app.InIsTTY)
		edit.text = &value
		return edit, err
	}

	return edit, nil
}

// readEditFields reads the source, URL, author, approximate mark and tags.
func readEditFields(in clikit.FieldValues) editFields {
	f := editFields{
		source: clikit.ReadFieldEdit(in, "source"), url: clikit.ReadFieldEdit(in, "url"), author: clikit.ReadFieldEdit(in, "author"),
		addTags: clikit.AppendUnique(nil, in.Values("add-tag")...), removeTags: in.Values("remove-tag"),
	}
	if approximate, given := in.Boolean("approximate"); given {
		f.approximate = &approximate
	}

	return f
}

// readEditTimes resolves --at and --ended as of env, rejecting an approximate --ended.
func readEditTimes(logger *slog.Logger, in clikit.FieldValues, env clikit.CheckEnv) (editTimes, error) {
	var times editTimes
	var echoes []string
	if raw := in.Value("at"); raw != nil {
		at, err := clikit.ResolveAt(raw, env.Now, env.Zone.Loc)
		if err != nil {
			return times, err
		}
		clikit.LogTypedTime(logger, clikit.TypedTime{Flag: "at", Input: *raw, Zone: env.Zone, Resolved: at})
		times.at, times.raw = &at, raw
		echoes = append(echoes, "at "+clikit.FormatResolved(at, env.Zone))
	}

	endedInput := in.Value("ended")
	times.clearEnded = endedInput != nil && *endedInput == clikit.ClearKeyword
	if endedInput != nil && !times.clearEnded {
		ended, err := clikit.ResolveEnded(*endedInput, env, editedEndedHint)
		if err != nil {
			return times, err
		}
		clikit.LogTypedTime(logger, clikit.TypedTime{Flag: "ended", Input: *endedInput, Zone: env.Zone, Resolved: ended})
		times.ended = &ended
		echoes = append(echoes, "ended "+clikit.FormatResolved(ended, env.Zone))
	}

	if len(echoes) > 0 {
		times.when = " " + strings.Join(echoes, ", ")
	}

	return times, nil
}

// apply returns ev with the edit made and updated at set, rejecting a result the store would,
// with times in z.
func (e eventEdit) apply(ev timeline.Event, z clikit.Zone) (timeline.Event, error) {
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
	ev.Source, ev.URL, ev.Author = e.source.Apply(ev.Source), e.url.Apply(ev.URL), e.author.Apply(ev.Author)
	ev.Tags = clikit.AppendUnique(slices.DeleteFunc(slices.Clone(ev.Tags), func(tag string) bool {
		return slices.Contains(e.removeTags, tag)
	}), e.addTags...)
	slices.Sort(ev.Tags)
	ev.UpdatedAt = e.now

	return ev, checkEditedEvent(ev, z)
}

// checkEditedEvent returns an ended at before occurred at as invalid_time, naming both in z, and
// any other rule ev breaks as the kind its rule names.
func checkEditedEvent(ev timeline.Event, z clikit.Zone) error {
	err := ev.Validate()
	if p, ok := clikit.FirstProblem(err); ok && p.Rule == timeline.RuleEndedBeforeOccurred {
		return &clikit.Error{
			Kind: clikit.KindOf(p.Rule),
			Message: fmt.Sprintf("the event would end at %s, before it occurred at %s",
				clikit.EndedText(ev, z), clikit.OccurredText(ev, z)),
			Hint: "Pass an --ended time no earlier than its occurred at, or --ended none.",
			Err:  err,
		}
	}

	return clikit.RecordError(err, "Run 'coroner event edit --help' for what each flag takes.")
}

// previewEventEdit prints ev as the edit would store it: its JSON object with attachments, or
// its summary.
func previewEventEdit(o clikit.Output, z clikit.Zone, ev timeline.Event, attachments []clikit.AttachmentJSON, t timeline.Timeline) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(NewJSON(ev, attachments))
	}

	_, err := io.WriteString(o.Out, eventSummary(ev, t, z))
	return err
}
