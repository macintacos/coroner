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
			"On a terminal, with no body or --kind, or with --interactive, a form asks for each flag, picks\n" +
			"the event from the timeline, and prints the command each entry equals. Under --json only\n" +
			"--interactive opens it, and --no-input never does.",
		Args:        annotationBodyArg,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner annotation add - -e 12 --kind evidence --source mongod < stepdown.log\n" +
			"  $ coroner annotation add \"Step-down lands 5 s after the COLLSCAN burst\" -e 12 --kind comment\n" +
			"  $ coroner annotation add \"Root cause: undersized oplog\" --kind comment --dry-run --json\n" +
			"  $ coroner annotation add --interactive -e 12",
	}

	return annotationAddForm().attach(ctx, cmd, app, terminalForms(app))
}

func annotationAddDefinition() definition {
	return definition{fields: []field{
		timelineField(),
		{name: "body", arg: true, required: true, usage: "The annotation's `body`, or - to read it from stdin", check: withoutEnv(checkBodyOrStdin)},
		{
			name: "event", shorthand: "e", check: withoutEnv(checkEventRef),
			usage: "Event to attach it to, as a `ref`: seq, timeline#seq or id (default: the timeline)",
		},
		{name: "kind", required: true, usage: "Annotation `kind`: {evidence | comment}", check: withoutEnv(checkAnnotationKindName)},
		{name: "source", usage: "System, service or person it came from, as a `source`, created on first use", check: withoutEnv(checkSource)},
		{name: "url", usage: "Permalink to the log line or message, as a `url`", check: notBlank("--url")},
		{name: "author", usage: "Who is adding it, as a `name` (default: config author, then USER)", check: notBlank("--author")},
		{name: "dry-run", typ: boolField, usage: "Show the annotation as it would be added, and add nothing"},
	}}
}

// annotationAdded is the annotation add stored or, with --dry-run, would store, with the
// timeline and the event, if any, it is attached to.
type annotationAdded struct {
	annotation timeline.Annotation
	timeline   timeline.Timeline
	event      *timeline.Event
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
	draft, err := draftAnnotation(in, app, cfg)
	if err != nil {
		return annotationAdded{}, err
	}
	dryRun, _ := in.boolean("dry-run")

	added := annotationAdded{dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		t, err := selectTimeline(ctx, s, in.value("timeline"), app.LookupEnv)
		if err != nil {
			return err
		}
		a := draft.annotation
		a.TimelineID = t.ID
		e, err := annotatedEvent(ctx, s, t, ref, timelineSelection{flag: in.value("timeline"), lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		if e != nil {
			a.EventID = &e.ID
		}
		added.annotation, added.timeline, added.event = a, t, e
		if dryRun {
			return nil
		}

		return s.AddAnnotation(ctx, a, draft.newSourceID)
	})

	return added, err
}

func (a annotationAdded) render(o output) error {
	target := annotationTarget(a.timeline, a.event)
	if a.dryRun {
		return previewAnnotation(o, a.annotation, fmt.Sprintf("Would add %s annotation to %s", a.annotation.Kind, target))
	}

	err := printAnnotation(o, a.annotation)
	if err != nil {
		return err
	}

	return o.notify("Added %s annotation %s to %s", a.annotation.Kind, a.annotation.ID, target)
}

// annotatedEvent returns the event ref names in t, or nil when ref is nil.
func annotatedEvent(
	ctx context.Context, s *store.Store, t timeline.Timeline, ref *eventRef, selection timelineSelection,
) (*timeline.Event, error) {
	var e *timeline.Event
	if ref != nil {
		found, err := findEventIn(ctx, s, t, *ref, selection)
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

// draftAnnotation reads the annotation in describes and gives it new ids. It reads stdin last,
// so a bad field never waits on it.
func draftAnnotation(in fieldValues, app App, cfg config.Config) (annotationDraft, error) {
	kind, err := requiredAnnotationKind(in)
	if err != nil {
		return annotationDraft{}, err
	}
	fields := readAnnotationFields(in)
	rawBody := in.value("body")
	if rawBody == nil {
		return annotationDraft{}, &Error{Kind: KindUsage, Message: "annotation add needs the annotation's body", Hint: missingBodyHint}
	}
	body, err := annotationBody(*rawBody, app.In, app.InIsTTY)
	if err != nil {
		return annotationDraft{}, err
	}
	id, err := app.NewID()
	if err != nil {
		return annotationDraft{}, err
	}
	sourceID, err := newSourceID(fields.source, app.NewID)
	if err != nil {
		return annotationDraft{}, err
	}

	now := app.Now()
	a := timeline.Annotation{
		ID:        id.String(),
		Kind:      kind,
		Body:      body,
		Author:    eventAuthor(in.value("author"), cfg.Author, app.LookupEnv),
		Source:    fields.source,
		URL:       fields.url,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return annotationDraft{annotation: a, newSourceID: sourceID}, nil
}

func requiredAnnotationKind(in fieldValues) (timeline.AnnotationKind, error) {
	kind := in.value("kind")
	if kind == nil {
		return "", &Error{Kind: KindUsage, Message: "annotation add needs --kind", Hint: annotationKindHint}
	}

	return checkAnnotationKind(*kind)
}

// annotationBodyArg rejects more than one argument, which is most often a body left unquoted.
func annotationBodyArg(_ *cobra.Command, args []string) error {
	if len(args) <= 1 {
		return nil
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("annotation add takes the body as one argument, got %d", len(args)),
		Hint:    "Quote a body that has spaces, or pass - to read it from stdin.",
	}
}
