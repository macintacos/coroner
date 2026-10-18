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
		Long: "Delete annotations by full id or unique id prefix, in any timeline. Every id is checked\n" +
			"before anything is deleted, so an id that matches nothing deletes none. --dry-run shows\n" +
			"what would be deleted instead.",
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
// their timelines' names by timeline id.
type annotationsDeleted struct {
	annotations   []timeline.Annotation
	timelineNames map[string]string
	dryRun        bool
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
		if err != nil || dryRun {
			return err
		}

		ids := make([]string, 0, len(annotations))
		for _, a := range annotations {
			ids = append(ids, a.ID)
		}
		err = s.DeleteAnnotations(ctx, ids...)
		if errors.Is(err, store.ErrNotFound) {
			return annotationNotFoundError("an annotation was deleted by another command first, so none were", err)
		}

		return err
	})

	return deleted, err
}

func (d annotationsDeleted) render(o output) error {
	if d.dryRun {
		return previewAnnotationDeletion(o, d.annotations, d.timelineNames)
	}

	return reportAnnotationDeletion(o, d.annotations, d.timelineNames)
}

// findAnnotations returns the annotation each id prefix names, in order and once each.
func findAnnotations(ctx context.Context, s *store.Store, prefixes []string) ([]timeline.Annotation, error) {
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
func annotationTimelineNames(ctx context.Context, s *store.Store, annotations []timeline.Annotation) (map[string]string, error) {
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

// previewAnnotationDeletion prints the annotations a delete would remove, as JSON or a line
// each.
func previewAnnotationDeletion(o output, annotations []timeline.Annotation, timelineNames map[string]string) error {
	if o.format == formatJSON {
		return o.json(newAnnotationListJSON(annotations))
	}

	for _, a := range annotations {
		_, err := fmt.Fprintf(o.out, "Would delete %s annotation %s from timeline `%s`\n", a.Kind, a.ID, timelineNames[a.TimelineID])
		if err != nil {
			return err
		}
	}

	return nil
}

// reportAnnotationDeletion prints the deleted annotations as JSON, or says on stderr what was
// deleted.
func reportAnnotationDeletion(o output, annotations []timeline.Annotation, timelineNames map[string]string) error {
	if o.format == formatJSON {
		return o.json(newAnnotationListJSON(annotations))
	}

	for _, a := range annotations {
		err := o.notify("Deleted %s annotation %s from timeline `%s`", a.Kind, a.ID, timelineNames[a.TimelineID])
		if err != nil {
			return err
		}
	}

	return nil
}
