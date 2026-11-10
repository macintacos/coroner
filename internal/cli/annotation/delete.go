package annotation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationDeleteCommand(ctx context.Context, app clikit.App) *cobra.Command {
	def := DeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <id>...",
		Short: "Delete annotations",
		Long: "Delete annotations by full id or unique id prefix, in any timeline, with their attachments\n" +
			"and the blobs no other attachment uses. Every id is checked before anything is deleted, so\n" +
			"an id that matches nothing deletes none. --dry-run shows what would be deleted instead.",
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{clikit.FormatsAnnotation: "text,json"},
		Example: "  $ coroner annotation delete 0199a1b2-7c1e --dry-run\n" +
			"  $ coroner annotation delete 0199a1b2-7c1e 0199a1b2-7d40 --json",
		RunE: clikit.WriteRunE(ctx, app, def, Delete),
	}

	def.AddFlags(cmd.Flags())

	return cmd
}

// DeleteDefinition declares `annotation delete`'s flags, argument and their checks.
func DeleteDefinition() clikit.Definition {
	return clikit.Definition{Fields: []clikit.Field{
		{
			Name: "ids", Arg: true, Typ: clikit.StringsField, Check: clikit.WithoutEnv(checkAnnotationRef),
			Usage: "Annotations to delete, as `id`s or unique id prefixes",
		},
		{Name: "dry-run", Typ: clikit.BoolField, Usage: "Show what would be deleted, and delete nothing"},
	}}
}

// Deleted are the annotations delete removed or, with --dry-run, would remove, with
// their timelines' names by timeline id, their attachments' objects by annotation id, what they
// sweep along, and why the blobs they left unused were not deleted.
type Deleted struct {
	annotations   []timeline.Annotation
	timelineNames map[string]string
	attached      map[string][]clikit.AttachmentJSON
	dryRun        bool
	swept         clikit.Sweep
	blobsErr      error
}

// LogChanges logs what the command changed, or would change under --dry-run.
func (d Deleted) LogChanges(ctx context.Context, logger *slog.Logger) {
	logger = clikit.WithDryRun(logger, d.dryRun)
	for _, a := range d.annotations {
		logger.InfoContext(ctx, "annotation deleted", clikit.AnnotationArgs(a)...)
	}
	d.swept.Log(ctx, logger)
}

// Delete runs `annotation delete` from in's values and returns what it changed, or would change under --dry-run.
func Delete(ctx context.Context, app clikit.App, cfg config.Config, in clikit.FieldValues) (Deleted, error) {
	err := DeleteDefinition().Check(in, clikit.CheckEnv{})
	if err != nil {
		return Deleted{}, err
	}
	if len(in.Values("ids")) == 0 {
		return Deleted{}, clikit.MissingValue("annotation delete", "at least one annotation id")
	}
	prefixes := make([]string, 0, len(in.Values("ids")))
	for _, ref := range in.Values("ids") {
		prefix, err := parseAnnotationRef(ref)
		if err != nil {
			return Deleted{}, err
		}
		prefixes = append(prefixes, prefix)
	}
	dryRun, _ := in.Boolean("dry-run")

	deleted, _, err := clikit.WithChange(ctx, app, cfg.DBPath, store.ChangeOptions{Mode: clikit.ModeFor(dryRun)},
		func(tx *store.Tx) (Deleted, error) {
			deleted := Deleted{dryRun: dryRun}
			annotations, err := findAnnotations(ctx, tx, prefixes)
			if err != nil {
				return deleted, err
			}
			deleted.annotations = annotations
			deleted.timelineNames, err = annotationTimelineNames(ctx, tx, annotations)
			if err != nil {
				return deleted, err
			}
			ids := make([]string, 0, len(annotations))
			owners := make([]timeline.AttachmentOwner, 0, len(annotations))
			for _, a := range annotations {
				ids = append(ids, a.ID)
				owners = append(owners, timeline.AttachmentOwner{AnnotationID: &a.ID})
			}
			attachments, err := clikit.OwnersAttachments(ctx, tx, owners)
			deleted.attached = clikit.AttachmentsByOwner(attachments, cfg.DBPath)
			deleted.swept = clikit.Sweep{Attachments: clikit.IDsOf(attachments, clikit.AttachmentID)}
			if err != nil {
				return deleted, err
			}

			return deleted, tx.DeleteAnnotations(ctx, ids...)
		})
	if err != nil {
		return deleted, err
	}
	if !dryRun {
		deleted.blobsErr = clikit.CollectBlobs(ctx, app, cfg.DBPath)
	}

	return deleted, nil
}

// Render prints the result in o's format.
func (d Deleted) Render(o clikit.Output) error {
	var err error
	switch {
	case o.Format == clikit.FormatJSON:
		err = o.JSON(newAnnotationListJSON(d.annotations, d.attached))
	case d.dryRun:
		return previewAnnotationDeletion(o, d.annotations, d.timelineNames)
	default:
		err = reportAnnotationDeletion(o, d.annotations, d.timelineNames)
	}
	if err != nil {
		return err
	}

	return clikit.NotifyBlobsErr(o, d.blobsErr)
}

// findAnnotations returns the annotation each id prefix names, in order and once each.
func findAnnotations(ctx context.Context, s clikit.Records, prefixes []string) ([]timeline.Annotation, error) {
	return clikit.FindEach(prefixes, clikit.AnnotationID, func(prefix string) (timeline.Annotation, error) {
		return findAnnotation(ctx, s, prefix)
	})
}

// annotationTimelineNames returns the name of each annotation's timeline, keyed by timeline id.
func annotationTimelineNames(ctx context.Context, s clikit.Records, annotations []timeline.Annotation) (map[string]string, error) {
	names := map[string]string{}
	for _, a := range annotations {
		if _, ok := names[a.TimelineID]; ok {
			continue
		}
		t, err := s.Timeline(ctx, a.TimelineID)
		if err != nil {
			return nil, err
		}
		names[t.ID] = t.Name
	}

	return names, nil
}

// previewAnnotationDeletion prints a line for each annotation a delete would remove.
func previewAnnotationDeletion(o clikit.Output, annotations []timeline.Annotation, timelineNames map[string]string) error {
	for _, a := range annotations {
		_, err := fmt.Fprintf(o.Out, "Would delete %s annotation %s from timeline `%s`\n", a.Kind, a.ID, timelineNames[a.TimelineID])
		if err != nil {
			return err
		}
	}

	return nil
}

// reportAnnotationDeletion says on stderr what was deleted.
func reportAnnotationDeletion(o clikit.Output, annotations []timeline.Annotation, timelineNames map[string]string) error {
	for _, a := range annotations {
		err := o.Notify("Deleted %s annotation %s from timeline `%s`", a.Kind, a.ID, timelineNames[a.TimelineID])
		if err != nil {
			return err
		}
	}

	return nil
}
