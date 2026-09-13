package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationDeleteCommand(ctx context.Context, app App) *cobra.Command {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteAnnotations(ctx, cmd, app, args)
		},
	}

	cmd.Flags().Bool("dry-run", false, "Show what would be deleted, and delete nothing")

	return cmd
}

func deleteAnnotations(ctx context.Context, cmd *cobra.Command, app App, refs []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}

	return withStore(ctx, cmd, app, func(s *store.Store) error {
		annotations, err := findAnnotations(ctx, s, refs)
		if err != nil {
			return err
		}
		timelineNames, err := annotationTimelineNames(ctx, s, annotations)
		if err != nil {
			return err
		}

		if !dryRun {
			ids := make([]string, 0, len(annotations))
			for _, a := range annotations {
				ids = append(ids, a.ID)
			}
			err = s.DeleteAnnotations(ctx, ids...)
			if err != nil {
				return err
			}
		}

		return reportAnnotationDeletion(o, annotations, timelineNames, dryRun)
	})
}

// findAnnotations returns the annotation each ref names, in order and once each.
func findAnnotations(ctx context.Context, s *store.Store, refs []string) ([]timeline.Annotation, error) {
	found := make([]timeline.Annotation, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		a, err := findAnnotation(ctx, s, ref)
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

// reportAnnotationDeletion prints the deleted annotations as JSON, or a line for each: on
// stdout for a dry run, as messages otherwise.
func reportAnnotationDeletion(o output, annotations []timeline.Annotation, timelineNames map[string]string, dryRun bool) error {
	if o.format == formatJSON {
		listed := make([]annotationJSON, 0, len(annotations))
		for _, a := range annotations {
			listed = append(listed, newAnnotationJSON(a))
		}
		return o.json(annotationListJSON{Annotations: listed})
	}

	for _, a := range annotations {
		line := fmt.Sprintf("%s annotation %s from timeline `%s`", a.Kind, a.ID, timelineNames[a.TimelineID])
		var err error
		if dryRun {
			_, err = fmt.Fprintln(o.out, "Would delete "+line)
		} else {
			err = o.notify("Deleted %s", line)
		}
		if err != nil {
			return err
		}
	}

	return nil
}
