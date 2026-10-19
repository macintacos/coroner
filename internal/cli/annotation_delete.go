package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationDeleteCommand(ctx context.Context, app App) *cobra.Command {
	def := annotationDeleteDefinition()
	cmd := &cobra.Command{
		Use:   "delete <id>...",
		Short: "Delete annotations",
		Long: "Delete annotations by full id or unique id prefix, in any timeline, with their attachments\n" +
			"and the blobs no other attachment uses. Every id is checked before anything is deleted, so\n" +
			"an id that matches nothing deletes none. --dry-run shows what would be deleted instead.",
		Args:        cobra.MinimumNArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner annotation delete 0199a1b2-7c1e --dry-run\n" +
			"  $ coroner annotation delete 0199a1b2-7c1e 0199a1b2-7d40 --json",
		RunE: writeRunE(ctx, app, def, deleteAnnotations),
	}

	def.addFlags(cmd.Flags())

	return cmd
}

func annotationDeleteDefinition() definition {
	return definition{fields: []field{
		{
			name: "ids", arg: true, typ: stringsField, check: withoutEnv(checkAnnotationRef),
			usage: "Annotations to delete, as `id`s or unique id prefixes",
		},
		{name: "dry-run", typ: boolField, usage: "Show what would be deleted, and delete nothing"},
	}}
}

// annotationsDeleted are the annotations delete removed or, with --dry-run, would remove, with
// their timelines' names by timeline id, their attachments' objects by annotation id, and why
// the blobs they left unused were not deleted.
type annotationsDeleted struct {
	annotations   []timeline.Annotation
	timelineNames map[string]string
	attached      map[string][]attachmentJSON
	dryRun        bool
	blobsErr      error
}

func deleteAnnotations(ctx context.Context, app App, cfg config.Config, in fieldValues) (annotationsDeleted, error) {
	err := annotationDeleteDefinition().check(in, checkEnv{})
	if err != nil {
		return annotationsDeleted{}, err
	}
	if len(in.values("ids")) == 0 {
		return annotationsDeleted{}, missingValue("annotation delete", "at least one annotation id")
	}
	prefixes := make([]string, 0, len(in.values("ids")))
	for _, ref := range in.values("ids") {
		prefix, err := parseAnnotationRef(ref)
		if err != nil {
			return annotationsDeleted{}, err
		}
		prefixes = append(prefixes, prefix)
	}
	dryRun, _ := in.boolean("dry-run")

	deleted := annotationsDeleted{dryRun: dryRun}
	err = withStorePath(ctx, app, cfg.DBPath, func(s *store.Store) error {
		annotations, err := findAnnotations(ctx, s, prefixes)
		if err != nil {
			return err
		}
		deleted.annotations = annotations
		deleted.timelineNames, err = annotationTimelineNames(ctx, s, annotations)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(annotations))
		owners := make([]timeline.AttachmentOwner, 0, len(annotations))
		for _, a := range annotations {
			ids = append(ids, a.ID)
			owners = append(owners, timeline.AttachmentOwner{AnnotationID: &a.ID})
		}
		attachments, err := ownersAttachments(ctx, s, owners)
		deleted.attached = attachmentsByOwner(attachments, cfg.DBPath)
		if err != nil || dryRun {
			return err
		}

		err = s.DeleteAnnotations(ctx, ids...)
		if errors.Is(err, store.ErrNotFound) {
			return annotationNotFoundError("an annotation was deleted by another command first, so none were", err)
		}
		if err != nil {
			return err
		}
		_, deleted.blobsErr = s.GarbageCollectBlobs(ctx)

		return nil
	})

	return deleted, err
}

func (d annotationsDeleted) render(o output) error {
	var err error
	switch {
	case o.format == formatJSON:
		err = o.json(newAnnotationListJSON(d.annotations, d.attached))
	case d.dryRun:
		return previewAnnotationDeletion(o, d.annotations, d.timelineNames)
	default:
		err = reportAnnotationDeletion(o, d.annotations, d.timelineNames)
	}
	if err != nil || d.blobsErr == nil {
		return err
	}

	return o.notify("Could not delete the blobs no attachment uses any more, so the next delete will: %v", d.blobsErr)
}

// findAnnotations returns the annotation each id prefix names, in order and once each.
func findAnnotations(ctx context.Context, s records, prefixes []string) ([]timeline.Annotation, error) {
	found := make([]timeline.Annotation, 0, len(prefixes))
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		a, err := findAnnotation(ctx, s, prefix)
		if err != nil {
			return nil, err
		}
		if !seen[a.ID] {
			seen[a.ID] = true
			found = append(found, a)
		}
	}

	return found, nil
}

// annotationTimelineNames returns the name of each annotation's timeline, keyed by timeline id.
func annotationTimelineNames(ctx context.Context, s records, annotations []timeline.Annotation) (map[string]string, error) {
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
func previewAnnotationDeletion(o output, annotations []timeline.Annotation, timelineNames map[string]string) error {
	for _, a := range annotations {
		_, err := fmt.Fprintf(o.out, "Would delete %s annotation %s from timeline `%s`\n", a.Kind, a.ID, timelineNames[a.TimelineID])
		if err != nil {
			return err
		}
	}

	return nil
}

// reportAnnotationDeletion says on stderr what was deleted.
func reportAnnotationDeletion(o output, annotations []timeline.Annotation, timelineNames map[string]string) error {
	for _, a := range annotations {
		err := o.notify("Deleted %s annotation %s from timeline `%s`", a.Kind, a.ID, timelineNames[a.TimelineID])
		if err != nil {
			return err
		}
	}

	return nil
}
