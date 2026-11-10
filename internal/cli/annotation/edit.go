package annotation

import (
	"context"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationEditCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := EditDefinition()
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change an annotation's body, kind, source or URL, or attach files",
		Long: "Change an annotation's body, kind, source or URL. Its author, event and timeline never\n" +
			"change. <id> is a full id or a unique id prefix, in any timeline. --body - reads the body\n" +
			"from stdin, which loses one trailing newline. --attach adds a file to the annotation's\n" +
			"attachments; 'coroner attachment delete' removes one.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner annotation edit 0199a1b2-7c1e --kind comment\n" +
			"  $ coroner annotation edit 0199a1b2-7c1e --attach ./stepdown-trace.pdf\n" +
			"  $ coroner annotation edit 0199a1b2-7c1e --body - --dry-run --json < stepdown.log",
		RunE: clikit.WriteRunE(ctx, app, def, Edit),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// EditDefinition declares `annotation edit`'s flags, argument and their checks.
func EditDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{Name: "id", Arg: true, Usage: "Annotation to edit, as an `id` or unique id prefix", Check: clikit.WithoutEnv(checkAnnotationRef)},
		{Name: "body", Usage: "New `body`, or - to read it from stdin", Check: clikit.WithoutEnv(clikit.AnnotationBodyInput().CheckOrStdin), Changes: true},
		{Name: "kind", Usage: "New annotation `kind`: {evidence | comment}", Check: clikit.WithoutEnv(checkAnnotationKindName), Changes: true},
		{Name: "source", Usage: "New `source`, created on first use, or none", Check: clikit.WithoutEnv(clikit.CheckEditedSource), Changes: true},
		{Name: "url", Usage: "New permalink, as a `url`, or none", Check: clikit.ClearableFreeText("--url"), Changes: true},
		clikit.Changing(clikit.AttachField()),
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show the edited annotation, and change nothing"},
	}}
}

// Edited is the annotation as edit stored it or, with --dry-run, would store it, with
// its timeline.
type Edited struct {
	annotation timeline.Annotation
	timeline   timeline.Timeline
	attach     clikit.AttachResult
	dryRun     bool
	fields     []string
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (e Edited) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, e.dryRun)
	logger.InfoContext(ctx, "annotation edited", append(clikit.AnnotationArgs(e.annotation), "fields", e.fields)...)
	e.attach.LogAdded(ctx, logger)
}

// Edit runs `annotation edit` from in's values and returns what it changed, or would change under --dry-run.
func Edit(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Edited, error) {
	err := EditDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Edited{}, err
	}
	id := in.Value("id")
	if id == nil {
		return Edited{}, clikit.MissingValue("annotation edit", "the annotation's id")
	}
	prefix, err := parseAnnotationRef(*id)
	if err != nil {
		return Edited{}, err
	}
	req, err := clikit.ReadAttachRequest(in, app.ReadFile)
	if err != nil {
		return Edited{}, err
	}
	edit, err := readAnnotationEdit(in, app)
	if err != nil {
		return Edited{}, err
	}
	now := app.Now()
	req, err = req.Staged(app.NewID, now)
	if err != nil {
		return Edited{}, err
	}

	ids := clikit.ReplayIDs{Draw: app.NewID}
	built, inserted, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(req.DryRun), Attach: req.ToAdd},
		func(tx *store.Tx) (clikit.Attached[Edited], error) {
			ids.Rewind()
			b := clikit.Attached[Edited]{Result: Edited{dryRun: req.DryRun}}
			a, err := findAnnotation(ctx, tx, prefix)
			if err != nil {
				return b, err
			}
			t, err := tx.Timeline(ctx, a.TimelineID)
			if err != nil {
				return b, err
			}
			before := a
			a, err = edit.apply(a, now)
			if err != nil {
				return b, err
			}
			b.Result.annotation, b.Result.timeline = a, t
			b.Result.fields = clikit.ChangedFields(export.NewAnnotation(before), export.NewAnnotation(a))
			// UpdateAnnotation resolves a.Source by name even when --source did not change it, so
			// it always needs a fallback id.
			sourceID, err := clikit.NewSourceID(a.Source, ids.NewID)
			if err != nil {
				return b, err
			}
			err = b.AttachIn(ctx, tx, timeline.AttachmentOwner{AnnotationID: &a.ID})
			if err != nil {
				return b, err
			}
			return b, tx.UpdateAnnotation(ctx, a, sourceID)
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
		err := previewAnnotation(o, e.annotation, e.attach.Objects, "Would edit annotation "+e.annotation.ID+" to")
		if err == nil {
			err = e.attach.Notify(o)
		}

		return err
	}

	err := printAnnotation(o, e.annotation, e.attach.Objects)
	if err == nil {
		err = e.attach.Notify(o)
	}
	if err != nil {
		return err
	}

	return o.Notify("Edited %s annotation %s in timeline `%s`", e.annotation.Kind, e.annotation.ID, e.timeline.Name)
}

// annotationEdit is the change edit's fields ask for. Nil fields change nothing.
type annotationEdit struct {
	kind        *timeline.AnnotationKind
	body        *string
	source, url clikit.FieldEdit
}

// readAnnotationEdit returns the edit in asks for. It reads stdin last, so a bad field never
// waits on it.
func readAnnotationEdit(in clikit.FieldValues, app clikit.App) (annotationEdit, error) {
	err := EditDefinition().RequireChange(in)
	if err != nil {
		return annotationEdit{}, err
	}

	edit := annotationEdit{source: clikit.ReadFieldEdit(in, "source"), url: clikit.ReadFieldEdit(in, "url")}
	if kind := in.Value("kind"); kind != nil {
		k := timeline.AnnotationKind(*kind)
		edit.kind = &k
	}
	if body := in.Value("body"); body != nil {
		b, err := clikit.AnnotationBodyInput().Read(*body, app.In, app.InIsTTY)
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
	a.Source = e.source.Apply(a.Source)
	a.URL = e.url.Apply(a.URL)
	a.UpdatedAt = now

	return a, clikit.RecordError(a.Validate(), "Run 'coroner annotation edit --help' for what each flag takes.")
}
