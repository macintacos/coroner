package cli

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

func newAnnotationEditCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change an annotation's body, kind, source or URL",
		Long: "Change an annotation's body, kind, source or URL. Its author, event and timeline never\n" +
			"change. <id> is a full id or a unique id prefix, in any timeline. --body - reads the body\n" +
			"from stdin, which loses one trailing newline.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{formatsAnnotation: "text,json"},
		Example: "  $ coroner annotation edit 0199a1b2-7c1e --kind comment\n" +
			"  $ coroner annotation edit 0199a1b2-7c1e --body - --dry-run --json < stepdown.log",
		RunE: func(cmd *cobra.Command, args []string) error {
			return editAnnotation(ctx, cmd, app, args[0])
		},
	}

	flags := cmd.Flags()
	flags.String("body", "", "New `body`, or - to read it from stdin")
	flags.String("kind", "", "New annotation `kind`: {evidence | comment}")
	flags.String("source", "", "New `source`, created on first use")
	flags.String("url", "", "New permalink, as a `url`")
	flags.Bool("dry-run", false, "Show the edited annotation, and change nothing")

	return cmd
}

func editAnnotation(ctx context.Context, cmd *cobra.Command, app App, ref string) error {
	prefix, err := parseAnnotationRef(ref)
	if err != nil {
		return err
	}
	edit, err := readAnnotationEdit(cmd.Flags(), app)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	o, err := newOutput(cmd, app)
	if err != nil {
		return err
	}

	return withStore(ctx, cmd, app, func(s *store.Store) error {
		// ponytail: find and update are separate transactions, so a concurrent edit of this
		// annotation is overwritten; apply the edit inside one store write if that matters.
		a, err := findAnnotation(ctx, s, prefix)
		if err != nil {
			return err
		}
		t, err := s.Timeline(ctx, a.TimelineID)
		if err != nil {
			return err
		}
		a = edit.apply(a, app.Now())
		if dryRun {
			return previewAnnotation(o, a, "Would edit annotation "+a.ID+" to")
		}

		// UpdateAnnotation resolves a.Source by name even when --source did not change it, so it
		// always needs a fallback id.
		sourceID, err := newSourceID(a.Source, app.NewID)
		if err != nil {
			return err
		}
		err = s.UpdateAnnotation(ctx, a, sourceID)
		if errors.Is(err, store.ErrNotFound) {
			return annotationNotFoundError("no annotation has an id starting "+a.ID, err)
		}
		if err != nil {
			return err
		}
		err = printAnnotation(o, a)
		if err != nil {
			return err
		}

		return o.notify("Edited %s annotation %s in timeline `%s`", a.Kind, a.ID, t.Name)
	})
}

// annotationEdit is the change edit's flags ask for. Nil fields change nothing.
type annotationEdit struct {
	kind   *timeline.AnnotationKind
	body   *string
	fields annotationFields
}

// readAnnotationEdit returns the edit the flags ask for, rejecting one no annotation could
// take. It reads stdin last, so a bad flag never waits on it.
func readAnnotationEdit(flags *pflag.FlagSet, app App) (annotationEdit, error) {
	var edit annotationEdit
	if !slices.ContainsFunc([]string{"body", "kind", "source", "url"}, flags.Changed) {
		return edit, &Error{Kind: KindUsage, Message: "nothing to edit", Hint: "Pass --body, --kind, --source or --url."}
	}

	if kind := changedString(flags, "kind"); kind != nil {
		k, err := checkAnnotationKind(*kind)
		if err != nil {
			return edit, err
		}
		edit.kind = &k
	}
	fields, err := readAnnotationFields(flags)
	if err != nil {
		return edit, err
	}
	edit.fields = fields
	if body := changedString(flags, "body"); body != nil {
		b, err := annotationBody(*body, app.In, app.InIsTTY)
		if err != nil {
			return edit, err
		}
		edit.body = &b
	}

	return edit, nil
}

// apply returns a with the edit made and updated at set to now.
func (e annotationEdit) apply(a timeline.Annotation, now time.Time) timeline.Annotation {
	if e.kind != nil {
		a.Kind = *e.kind
	}
	if e.body != nil {
		a.Body = *e.body
	}
	if e.fields.source != nil {
		a.Source = e.fields.source
	}
	if e.fields.url != nil {
		a.URL = e.fields.url
	}
	a.UpdatedAt = now

	return a
}
