package annotation

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const missingBodyHint = "Pass the body, or - to read it from stdin:\n" +
	"  $ coroner annotation add \"Step-down lands 5 s after the COLLSCAN burst\" --kind comment\n" +
	"  $ coroner annotation add - -e 12 --kind evidence < stepdown.log\n" +
	"Run 'coroner annotation add --help' for more."

func newAnnotationAddCommand(ctx context.Context, app clikit.App, newTimeline clikit.TimelineAsk) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add {<body> | -} --kind {evidence | comment}",
		Short: "Attach evidence or a comment to an event or the timeline",
		Long: "Attach an annotation to the event -e names, or to the whole timeline without -e. Evidence\n" +
			"is kept verbatim: pass - to read a log line or chat message from stdin, which loses one\n" +
			"trailing newline and is never read otherwise.\n\n" +
			"-e takes a seq such as 12, a timeline and seq such as mongo-outage#12, or an event id or\n" +
			"unique id prefix. The event must be in the timeline the annotation goes to.\n\n" +
			"Each --attach copies a file, such as a screenshot, into the blob directory beside the\n" +
			"database, and prints its id, sha256, size and type.\n\n" +
			"On a terminal, with no body or --kind, or with --interactive, a form asks for each flag,\n" +
			"suggests sources, the ones on the timeline's latest events first, picks the event from the\n" +
			"timeline, and prints the command each entry equals. Under --json only\n" +
			"--interactive opens it, and --no-input never does.",
		Args:        clikit.AnnotationBodyInput().Args,
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner annotation add - -e 12 --kind evidence --source mongod < stepdown.log\n" +
			"  $ coroner annotation add \"Step-down lands 5 s after the COLLSCAN burst\" -e 12 --kind comment\n" +
			"  $ coroner annotation add \"Grafana panel at the step-down\" -e 12 --kind evidence --attach ./panel.png\n" +
			"  $ coroner annotation add \"Root cause: undersized oplog\" --kind comment --dry-run --json\n" +
			"  $ coroner annotation add --interactive -e 12",
	}

	return AddForm(newTimeline).Attach(ctx, cmd, app, clikit.TerminalForms(app, "💬 New annotation"))
}

// AddDefinition declares `annotation add`'s flags, argument and their checks.
func AddDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		clikit.TimelineField(),
		{Name: "body", Arg: true, Required: true, PerEntry: true, Usage: "The annotation's `body`, or - to read it from stdin", Check: clikit.WithoutEnv(clikit.AnnotationBodyInput().CheckOrStdin)},
		{
			Name: "event", Shorthand: "e", Check: clikit.WithoutEnv(clikit.CheckEventRef),
			Usage: "Event to attach it to, as a `ref`: seq, timeline#seq or id (default: the timeline)",
		},
		{Name: "kind", Required: true, Usage: "Annotation `kind`: {evidence | comment}", Check: clikit.WithoutEnv(checkAnnotationKindName)},
		{Name: "source", Usage: "System, service or person it came from, as a `source`, created on first use", Check: clikit.WithoutEnv(clikit.CheckSource)},
		{Name: "url", PerEntry: true, Usage: "Permalink to the log line or message, as a `url`", Check: clikit.FreeText("--url")},
		{Name: "author", Usage: "Who is adding it, as a `name` (default: config author, then USER)", Check: clikit.FreeText("--author")},
		clikit.PerEntry(clikit.AttachField()),
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the annotation as it would be added, and add nothing"},
	}}
}

// annotationAdded is the annotation add stored or, with --dry-run, would store, with the
// timeline and the event, if any, it is attached to.
type annotationAdded struct {
	annotation timeline.Annotation
	timeline   timeline.Timeline
	event      *timeline.Event
	attach     clikit.AttachResult
	dryRun     bool
}

func (a annotationAdded) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, a.dryRun)
	logger.InfoContext(ctx, "annotation added", clikit.AnnotationArgs(a.annotation)...)
	a.attach.LogAdded(ctx, logger)
}

func addAnnotation(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (annotationAdded, error) {
	err := AddDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return annotationAdded{}, err
	}
	ref, err := clikit.OptionalEventRef(in.Value("event"))
	if err != nil {
		return annotationAdded{}, err
	}
	req, err := clikit.ReadAttachRequest(in, app.ReadFile)
	if err != nil {
		return annotationAdded{}, err
	}
	draft, err := draftAnnotation(in, app, cfg)
	if err != nil {
		return annotationAdded{}, err
	}
	req, err = req.Staged(app.NewID, draft.annotation.CreatedAt)
	if err != nil {
		return annotationAdded{}, err
	}

	owner := timeline.AttachmentOwner{AnnotationID: &draft.annotation.ID}
	built, inserted, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(req.DryRun), Attach: req.ToAdd},
		func(tx *store.Tx) (clikit.Attached[annotationAdded], error) {
			b := clikit.Attached[annotationAdded]{Result: annotationAdded{dryRun: req.DryRun}}
			t, err := clikit.SelectOne(ctx, app.Logger(), tx, clikit.TimelineInputOf(in), app.LookupEnv)
			if err != nil {
				return b, err
			}
			a := draft.annotation
			a.TimelineID = t.ID
			e, err := annotatedEvent(ctx, tx, t, ref)
			if err != nil {
				return b, err
			}
			if e != nil {
				a.EventID = &e.ID
			}
			b.Result.annotation, b.Result.timeline, b.Result.event = a, t, e
			err = b.AttachIn(ctx, tx, owner)
			if err != nil {
				return b, err
			}
			return b, tx.AddAnnotation(ctx, a, draft.newSourceID)
		})
	added := built.Result
	if err != nil {
		return added, err
	}
	added.attach = built.AttachResult(req, inserted, cfg.DBPath)

	return added, nil
}

func (a annotationAdded) Render(o clikit.Output) error {
	target := annotationTarget(a.timeline, a.event)
	if a.dryRun {
		err := previewAnnotation(o, a.annotation, a.attach.Objects, fmt.Sprintf("Would add %s annotation to %s", a.annotation.Kind, target))
		if err == nil {
			err = a.attach.Notify(o)
		}

		return err
	}

	err := printAnnotation(o, a.annotation, a.attach.Objects)
	if err == nil {
		err = o.PrintID(a.annotation.ID)
	}
	if err == nil {
		err = a.attach.Notify(o)
	}
	if err != nil {
		return err
	}

	return o.Notify("Added %s annotation %s to %s", a.annotation.Kind, a.annotation.ID, target)
}

func (a annotationAdded) Summary() (clikit.SummaryLine, bool) {
	cells := []string{string(a.annotation.Kind), clikit.PickerLabel(a.annotation.Body)}
	return clikit.SummaryLine{Timeline: a.timeline.Name, Cells: cells}, !a.dryRun
}

// annotatedEvent returns the event ref names in t, or nil when ref is nil.
func annotatedEvent(
	ctx context.Context, s clikit.Records, t timeline.Timeline, ref *clikit.EventRef,
) (*timeline.Event, error) {
	var e *timeline.Event
	if ref != nil {
		found, err := clikit.FindEventIn(ctx, s, t, *ref)
		if err != nil {
			return nil, err
		}
		e = &found
	}

	return e, nil
}

// annotationDraft is an annotation read from its fields and checked, still without its timeline
// and event.
type annotationDraft struct {
	annotation timeline.Annotation
	// newSourceID is the id annotation.Source gets when no source has that name yet.
	newSourceID string
}

// draftAnnotation reads the annotation in describes, checks it, and gives it new ids. It reads
// stdin after every field check, so a bad field never waits on it.
func draftAnnotation(in clikit.FieldValues, app clikit.App, cfg config.Config) (annotationDraft, error) {
	kind := in.Value("kind")
	if kind == nil {
		return annotationDraft{}, &clikit.Error{Kind: clikit.KindUsage, Message: "annotation add needs --kind", Hint: annotationKindHint}
	}
	fields := readAnnotationFields(in)
	rawBody := in.Value("body")
	if rawBody == nil {
		return annotationDraft{}, &clikit.Error{Kind: clikit.KindUsage, Message: "annotation add needs the annotation's body", Hint: missingBodyHint}
	}
	author, err := clikit.DefaultAuthor(in.Value("author"), cfg.Author, app.LookupEnv)
	if err != nil {
		return annotationDraft{}, err
	}
	body, err := clikit.AnnotationBodyInput().Read(*rawBody, app.In, app.InIsTTY)
	if err != nil {
		return annotationDraft{}, err
	}
	now := app.Now()
	a := timeline.Annotation{
		Kind:      timeline.AnnotationKind(*kind),
		Body:      body,
		Author:    author,
		Source:    fields.source,
		URL:       fields.url,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err = clikit.RecordError(a.Validate(), "Run 'coroner annotation add --help' for what each flag takes.")
	if err != nil {
		return annotationDraft{}, err
	}
	id, err := app.NewID()
	if err != nil {
		return annotationDraft{}, err
	}
	a.ID = id.String()
	sourceID, err := clikit.NewSourceID(fields.source, app.NewID)
	if err != nil {
		return annotationDraft{}, err
	}

	return annotationDraft{annotation: a, newSourceID: sourceID}, nil
}

// AddForm returns `annotation add`'s form, whose timeline picker creates a timeline through newTimeline.
func AddForm(newTimeline clikit.TimelineAsk) clikit.AddForm[annotationAdded] {
	return clikit.AddForm[annotationAdded]{
		Path: "annotation add", Def: AddDefinition(), Run: addAnnotation,
		Choices: Choices, NewTimeline: newTimeline,
	}
}

// Choices suggests sources, picks the kind, and picks the event from t's events, or none
// for the whole timeline.
func Choices(ctx context.Context, s *store.Store, t timeline.Timeline, z clikit.Zone) (clikit.FormChoices, error) {
	choices, err := clikit.SourceChoices(ctx, s, t)
	if err != nil {
		return choices, err
	}
	events, err := s.Events(ctx, t.ID)
	if err != nil {
		return choices, err
	}
	// ponytail: huh draws every option on each key, so only the last 200 events are offered; a
	// filtered query replaces the cap when older ones need picking. A typed -e still reaches any.
	events = events[max(0, len(events)-200):]

	eventOptions := []huh.Option[string]{huh.NewOption("none (the whole timeline)", "")}
	for _, e := range events {
		seq := strconv.FormatInt(e.Seq, 10)
		eventOptions = append(eventOptions, huh.NewOption("#"+seq+"  "+clikit.OccurredText(e, z)+"  "+clikit.PickerLabel(e.Text), seq))
	}
	choices.Options = map[string][]huh.Option[string]{
		"event": eventOptions,
		"kind": {
			huh.NewOption("choose evidence or comment", ""),
			huh.NewOption(string(timeline.AnnotationEvidence), string(timeline.AnnotationEvidence)),
			huh.NewOption(string(timeline.AnnotationComment), string(timeline.AnnotationComment)),
		},
	}

	return choices, nil
}
