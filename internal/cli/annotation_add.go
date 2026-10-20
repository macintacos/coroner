package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const missingBodyHint = "Pass the body, or - to read it from stdin:\n" +
	"  $ coroner annotation add \"Step-down lands 5 s after the COLLSCAN burst\" --kind comment\n" +
	"  $ coroner annotation add - -e 12 --kind evidence < stepdown.log\n" +
	"Run 'coroner annotation add --help' for more."

func newAnnotationAddCommand(ctx context.Context, app App) *cobra.Command {
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
		Args:        annotationBodyInput().args,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner annotation add - -e 12 --kind evidence --source mongod < stepdown.log\n" +
			"  $ coroner annotation add \"Step-down lands 5 s after the COLLSCAN burst\" -e 12 --kind comment\n" +
			"  $ coroner annotation add \"Grafana panel at the step-down\" -e 12 --kind evidence --attach ./panel.png\n" +
			"  $ coroner annotation add \"Root cause: undersized oplog\" --kind comment --dry-run --json\n" +
			"  $ coroner annotation add --interactive -e 12",
	}

	return annotationAddForm().attach(ctx, cmd, app, terminalForms(app, "💬 New annotation"))
}

func annotationAddDefinition() definition {
	return definition{fields: []field{
		timelineField(),
		{name: "body", arg: true, required: true, perEntry: true, usage: "The annotation's `body`, or - to read it from stdin", check: withoutEnv(annotationBodyInput().checkOrStdin)},
		{
			name: "event", shorthand: "e", check: withoutEnv(checkEventRef),
			usage: "Event to attach it to, as a `ref`: seq, timeline#seq or id (default: the timeline)",
		},
		{name: "kind", required: true, usage: "Annotation `kind`: {evidence | comment}", check: withoutEnv(checkAnnotationKindName)},
		{name: "source", usage: "System, service or person it came from, as a `source`, created on first use", check: withoutEnv(checkSource)},
		{name: "url", perEntry: true, usage: "Permalink to the log line or message, as a `url`", check: freeText("--url")},
		{name: "author", usage: "Who is adding it, as a `name` (default: config author, then USER)", check: freeText("--author")},
		perEntry(attachField()),
		{name: "dry-run", typ: boolField, usage: "Show the annotation as it would be added, and add nothing"},
	}}
}

// annotationAdded is the annotation add stored or, with --dry-run, would store, with the
// timeline and the event, if any, it is attached to.
type annotationAdded struct {
	annotation timeline.Annotation
	timeline   timeline.Timeline
	event      *timeline.Event
	attach     attachResult
	dryRun     bool
}

func addAnnotation(ctx context.Context, app App, cfg config.Config, in fieldValues) (annotationAdded, error) {
	err := annotationAddDefinition().check(in, checkEnv{})
	if err != nil {
		return annotationAdded{}, err
	}
	ref, err := optionalEventRef(in.value("event"))
	if err != nil {
		return annotationAdded{}, err
	}
	req, err := readAttachRequest(in, app.ReadFile)
	if err != nil {
		return annotationAdded{}, err
	}
	draft, err := draftAnnotation(in, app, cfg)
	if err != nil {
		return annotationAdded{}, err
	}
	req, err = req.staged(app.NewID, draft.annotation.CreatedAt)
	if err != nil {
		return annotationAdded{}, err
	}

	owner := timeline.AttachmentOwner{AnnotationID: &draft.annotation.ID}
	built, inserted, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(req.dryRun), Attach: req.toAdd},
		func(tx *store.Tx) (attached[annotationAdded], error) {
			b := attached[annotationAdded]{result: annotationAdded{dryRun: req.dryRun}}
			t, err := selectOne(ctx, tx, timelineInputOf(in), app.LookupEnv)
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
			b.result.annotation, b.result.timeline, b.result.event = a, t, e
			err = b.attachIn(ctx, tx, owner)
			if err != nil {
				return b, err
			}
			return b, tx.AddAnnotation(ctx, a, draft.newSourceID)
		})
	added := built.result
	if err != nil {
		return added, err
	}
	added.attach = built.attachResult(req, inserted, cfg.DBPath)

	return added, nil
}

func (a annotationAdded) render(o output) error {
	target := annotationTarget(a.timeline, a.event)
	if a.dryRun {
		err := previewAnnotation(o, a.annotation, a.attach.objects, fmt.Sprintf("Would add %s annotation to %s", a.annotation.Kind, target))
		if err == nil {
			err = a.attach.notify(o)
		}

		return err
	}

	err := printAnnotation(o, a.annotation, a.attach.objects)
	if err == nil {
		err = o.printID(a.annotation.ID)
	}
	if err == nil {
		err = a.attach.notify(o)
	}
	if err != nil {
		return err
	}

	return o.notify("Added %s annotation %s to %s", a.annotation.Kind, a.annotation.ID, target)
}

func (a annotationAdded) summary() (summaryLine, bool) {
	line := string(a.annotation.Kind) + "  " + pickerLabel(a.annotation.Body)
	return summaryLine{timeline: a.timeline.Name, text: line}, !a.dryRun
}

// annotatedEvent returns the event ref names in t, or nil when ref is nil.
func annotatedEvent(
	ctx context.Context, s records, t timeline.Timeline, ref *eventRef,
) (*timeline.Event, error) {
	var e *timeline.Event
	if ref != nil {
		found, err := findEventIn(ctx, s, t, *ref)
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
func draftAnnotation(in fieldValues, app App, cfg config.Config) (annotationDraft, error) {
	kind := in.value("kind")
	if kind == nil {
		return annotationDraft{}, &Error{Kind: KindUsage, Message: "annotation add needs --kind", Hint: annotationKindHint}
	}
	fields := readAnnotationFields(in)
	rawBody := in.value("body")
	if rawBody == nil {
		return annotationDraft{}, &Error{Kind: KindUsage, Message: "annotation add needs the annotation's body", Hint: missingBodyHint}
	}
	author, err := defaultAuthor(in.value("author"), cfg.Author, app.LookupEnv)
	if err != nil {
		return annotationDraft{}, err
	}
	body, err := annotationBodyInput().read(*rawBody, app.In, app.InIsTTY)
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
	err = recordError(a.Validate(), "Run 'coroner annotation add --help' for what each flag takes.")
	if err != nil {
		return annotationDraft{}, err
	}
	id, err := app.NewID()
	if err != nil {
		return annotationDraft{}, err
	}
	a.ID = id.String()
	sourceID, err := newSourceID(fields.source, app.NewID)
	if err != nil {
		return annotationDraft{}, err
	}

	return annotationDraft{annotation: a, newSourceID: sourceID}, nil
}
