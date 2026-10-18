package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
			"unique id prefix. The event must be in the timeline the annotation goes to.",
		Args:        annotationBodyArg,
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner annotation add - -e 12 --kind evidence --source mongod < stepdown.log\n" +
			"  $ coroner annotation add \"Step-down lands 5 s after the COLLSCAN burst\" -e 12 --kind comment\n" +
			"  $ coroner annotation add \"Root cause: undersized oplog\" --kind comment --dry-run --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return addAnnotation(ctx, cmd, app, args)
		},
	}

	flags := cmd.Flags()
	addTimelineFlag(flags)
	flags.StringP("event", "e", "", "Event to attach it to, as a `ref`: seq, timeline#seq or id (default: the timeline)")
	flags.String("kind", "", "Annotation `kind`: {evidence | comment}")
	flags.String("source", "", "System, service or person it came from, as a `source`, created on first use")
	flags.String("url", "", "Permalink to the log line or message, as a `url`")
	flags.String("author", "", "Who is adding it, as a `name` (default: config author, then USER)")
	flags.Bool("dry-run", false, "Show the annotation as it would be added, and add nothing")

	return cmd
}

func addAnnotation(ctx context.Context, cmd *cobra.Command, app App, args []string) error {
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
	ref, err := eventFlag(cmd.Flags())
	if err != nil {
		return err
	}
	draft, err := draftAnnotation(cmd.Flags(), app, cfg, args)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	return withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		t, err := selectTimeline(ctx, s, flag, app.LookupEnv)
		if err != nil {
			return err
		}
		a := draft.annotation
		a.TimelineID = t.ID
		e, err := annotatedEvent(ctx, s, t, ref, timelineSelection{flag: flag, lookupEnv: app.LookupEnv})
		if err != nil {
			return err
		}
		if e != nil {
			a.EventID = &e.ID
		}
		if dryRun {
			return previewAnnotation(o, a, fmt.Sprintf("Would add %s annotation to %s", a.Kind, annotationTarget(t, e)))
		}

		err = s.AddAnnotation(ctx, a, draft.newSourceID)
		if err != nil {
			return err
		}
		err = printAnnotation(o, a)
		if err != nil {
			return err
		}

		return o.notify("Added %s annotation %s to %s", a.Kind, a.ID, annotationTarget(t, e))
	})
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

// annotationDraft is an annotation read from the command line and checked, still without its
// timeline and event.
type annotationDraft struct {
	annotation timeline.Annotation
	// newSourceID is the id annotation.Source gets when no source has that name yet.
	newSourceID string
}

// draftAnnotation reads the annotation args and flags describe and gives it new ids. It reads
// stdin last, so a bad flag never waits on it.
func draftAnnotation(flags *pflag.FlagSet, app App, cfg config.Config, args []string) (annotationDraft, error) {
	kind, err := requiredAnnotationKind(flags)
	if err != nil {
		return annotationDraft{}, err
	}
	fields, err := readAnnotationFields(flags)
	if err != nil {
		return annotationDraft{}, err
	}
	author := changedString(flags, "author")
	err = checkNotBlank("--author", author)
	if err != nil {
		return annotationDraft{}, err
	}
	if len(args) == 0 {
		return annotationDraft{}, &Error{Kind: KindUsage, Message: "annotation add needs the annotation's body", Hint: missingBodyHint}
	}
	body, err := annotationBody(args[0], app.In, app.InIsTTY)
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
		Author:    eventAuthor(author, cfg.Author, app.LookupEnv),
		Source:    fields.source,
		URL:       fields.url,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return annotationDraft{annotation: a, newSourceID: sourceID}, nil
}

func requiredAnnotationKind(flags *pflag.FlagSet) (timeline.AnnotationKind, error) {
	kind := changedString(flags, "kind")
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
