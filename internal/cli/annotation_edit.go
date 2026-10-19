package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationEditCommand(ctx context.Context, app App) *cobra.Command {
	def := annotationEditDefinition()
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change an annotation's body, kind, source or URL, or attach files",
		Long: "Change an annotation's body, kind, source or URL. Its author, event and timeline never\n" +
			"change. <id> is a full id or a unique id prefix, in any timeline. --body - reads the body\n" +
			"from stdin, which loses one trailing newline. --attach adds a file to the annotation's\n" +
			"attachments; 'coroner attachment delete' removes one.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner annotation edit 0199a1b2-7c1e --kind comment\n" +
			"  $ coroner annotation edit 0199a1b2-7c1e --attach ./stepdown-trace.pdf\n" +
			"  $ coroner annotation edit 0199a1b2-7c1e --body - --dry-run --json < stepdown.log",
		RunE: writeRunE(ctx, app, def, editAnnotation),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func annotationEditDefinition() definition {
	return definition{fields: []field{
		{name: "id", arg: true, usage: "Annotation to edit, as an `id` or unique id prefix", check: withoutEnv(checkAnnotationRef)},
		{name: "body", usage: "New `body`, or - to read it from stdin", check: withoutEnv(annotationBodyInput().checkOrStdin), changes: true},
		{name: "kind", usage: "New annotation `kind`: {evidence | comment}", check: withoutEnv(checkAnnotationKindName), changes: true},
		{name: "source", usage: "New `source`, created on first use, or none", check: withoutEnv(checkEditedSource), changes: true},
		{name: "url", usage: "New permalink, as a `url`, or none", check: clearableFreeText("--url"), changes: true},
		changing(attachField()),
		{name: "dry-run", typ: boolField, usage: "Show the edited annotation, and change nothing"},
	}}
}

// annotationEdited is the annotation as edit stored it or, with --dry-run, would store it, with
// its timeline.
type annotationEdited struct {
	annotation timeline.Annotation
	timeline   timeline.Timeline
	attach     attachResult
	dryRun     bool
}

func editAnnotation(ctx context.Context, app App, cfg config.Config, in fieldValues) (annotationEdited, error) {
	err := annotationEditDefinition().check(in, checkEnv{})
	if err != nil {
		return annotationEdited{}, err
	}
	id := in.value("id")
	if id == nil {
		return annotationEdited{}, missingValue("annotation edit", "the annotation's id")
	}
	prefix, err := parseAnnotationRef(*id)
	if err != nil {
		return annotationEdited{}, err
	}
	req, err := readAttachRequest(in, app.ReadFile)
	if err != nil {
		return annotationEdited{}, err
	}
	edit, err := readAnnotationEdit(in, app)
	if err != nil {
		return annotationEdited{}, err
	}
	now := app.Now()
	req, err = req.staged(app.NewID, now)
	if err != nil {
		return annotationEdited{}, err
	}

	ids := replayIDs{draw: app.NewID}
	built, inserted, err := withChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: modeFor(req.dryRun), Attach: req.toAdd},
		func(tx *store.Tx) (attached[annotationEdited], error) {
			ids.rewind()
			b := attached[annotationEdited]{result: annotationEdited{dryRun: req.dryRun}}
			a, err := findAnnotation(ctx, tx, prefix)
			if err != nil {
				return b, err
			}
			t, err := tx.Timeline(ctx, a.TimelineID)
			if err != nil {
				return b, err
			}
			a, err = edit.apply(a, now)
			if err != nil {
				return b, err
			}
			b.result.annotation, b.result.timeline = a, t
			// UpdateAnnotation resolves a.Source by name even when --source did not change it, so
			// it always needs a fallback id.
			sourceID, err := newSourceID(a.Source, ids.newID)
			if err != nil {
				return b, err
			}
			err = b.attachIn(ctx, tx, timeline.AttachmentOwner{AnnotationID: &a.ID})
			if err != nil {
				return b, err
			}
			return b, tx.UpdateAnnotation(ctx, a, sourceID)
		})
	edited := built.result
	if err != nil {
		return edited, err
	}
	edited.attach = built.attachResult(req, inserted, cfg.DBPath)

	return edited, nil
}

func (e annotationEdited) render(o output) error {
	if e.dryRun {
		err := previewAnnotation(o, e.annotation, e.attach.objects, "Would edit annotation "+e.annotation.ID+" to")
		if err == nil {
			err = e.attach.notify(o)
		}

		return err
	}

	err := printAnnotation(o, e.annotation, e.attach.objects)
	if err == nil {
		err = e.attach.notify(o)
	}
	if err != nil {
		return err
	}

	return o.notify("Edited %s annotation %s in timeline `%s`", e.annotation.Kind, e.annotation.ID, e.timeline.Name)
}

// annotationEdit is the change edit's fields ask for. Nil fields change nothing.
type annotationEdit struct {
	kind        *timeline.AnnotationKind
	body        *string
	source, url fieldEdit
}

// readAnnotationEdit returns the edit in asks for. It reads stdin last, so a bad field never
// waits on it.
func readAnnotationEdit(in fieldValues, app App) (annotationEdit, error) {
	err := annotationEditDefinition().requireChange(in)
	if err != nil {
		return annotationEdit{}, err
	}

	edit := annotationEdit{source: readFieldEdit(in, "source"), url: readFieldEdit(in, "url")}
	if kind := in.value("kind"); kind != nil {
		k := timeline.AnnotationKind(*kind)
		edit.kind = &k
	}
	if body := in.value("body"); body != nil {
		b, err := annotationBodyInput().read(*body, app.In, app.InIsTTY)
		if err != nil {
			return edit, err
		}
		edit.body = &b
	}

	return edit, nil
}

// apply returns a with the edit made and updated at set to now, rejecting a result the store
// would.
func (e annotationEdit) apply(a timeline.Annotation, now time.Time) (timeline.Annotation, error) {
	if e.kind != nil {
		a.Kind = *e.kind
	}
	if e.body != nil {
		a.Body = *e.body
	}
	a.Source = e.source.apply(a.Source)
	a.URL = e.url.apply(a.URL)
	a.UpdatedAt = now

	return a, recordError(a.Validate(), "Run 'coroner annotation edit --help' for what each flag takes.")
}
