// Package annotation holds the `coroner annotation` commands.
package annotation

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	annotationKindHint  = "Pass --kind evidence for raw material such as a log line, or --kind comment for commentary."
	listAnnotationsHint = "Run 'coroner annotation list' to see a timeline's annotations and their ids."
)

// NewCommand returns the annotation command and its verbs, which run against app.
func NewCommand(ctx context.Context, app clikit.App, newTimeline clikit.TimelineAsk) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "annotation",
		Short:   "Attach evidence and comments to events and timelines",
		GroupID: clikit.CoreCommands,
		Long: "An annotation is evidence, raw material kept verbatim such as a log line, or a comment,\n" +
			"attached to an event or to the whole timeline. It has no time of its own in the\n" +
			"chronology.",
		Example: "  $ pbpaste | coroner annotation add - -e 12 --kind evidence --source mongod",
		RunE:    clikit.RunNoun,
	}

	cmd.AddCommand(
		newAnnotationAddCommand(ctx, app, newTimeline),
		newAnnotationListCommand(ctx, app),
		newAnnotationEditCommand(ctx, app),
		newAnnotationDeleteCommand(ctx, app),
	)

	return cmd
}

// JSON is the export annotation with its output-only attachments.
type JSON struct {
	export.Annotation

	// Never nil, so no attachments encode as [] rather than null.
	Attachments []clikit.AttachmentJSON `json:"attachments"`
}

// NewJSON returns a with its attachments as --json prints it.
func NewJSON(a timeline.Annotation, attachments []clikit.AttachmentJSON) JSON {
	return JSON{Annotation: export.NewAnnotation(a), Attachments: append([]clikit.AttachmentJSON{}, attachments...)}
}

type annotationListJSON struct {
	Annotations []JSON `json:"annotations"`
}

// newAnnotationListJSON returns annotations' objects, each with its attachments from attached,
// keyed by annotation id.
func newAnnotationListJSON(annotations []timeline.Annotation, attached map[string][]clikit.AttachmentJSON) annotationListJSON {
	listed := make([]JSON, 0, len(annotations))
	for _, a := range annotations {
		listed = append(listed, NewJSON(a, attached[a.ID]))
	}

	return annotationListJSON{Annotations: listed}
}

// printAnnotation prints a with attachments when o asks for JSON; text output leaves it to the
// command's message.
func printAnnotation(o clikit.Output, a timeline.Annotation, attachments []clikit.AttachmentJSON) error {
	if o.Format != clikit.FormatJSON {
		return nil
	}

	return o.JSON(NewJSON(a, attachments))
}

// previewAnnotation prints a as a dry run would store it: its JSON object with attachments, or
// heading and a description.
func previewAnnotation(o clikit.Output, a timeline.Annotation, attachments []clikit.AttachmentJSON, heading string) error {
	if o.Format == clikit.FormatJSON {
		return o.JSON(NewJSON(a, attachments))
	}

	var b strings.Builder
	b.WriteString(heading + "\n")
	clikit.WriteField(&b, "Kind:", string(a.Kind))
	clikit.WriteField(&b, "Source:", clikit.OrDash(a.Source))
	clikit.WriteField(&b, "URL:", clikit.OrDash(a.URL))
	clikit.WriteField(&b, "Author:", clikit.OrDash(a.Author))
	clikit.WriteField(&b, "Body:", a.Body)

	_, err := io.WriteString(o.Out, b.String())
	return err
}

// checkAnnotationKindName returns usage naming the kinds there are unless kind is one.
func checkAnnotationKindName(kind string) error {
	if timeline.AnnotationKind(kind).Valid() {
		return nil
	}

	return &clikit.Error{Kind: clikit.KindUsage, Message: fmt.Sprintf("--kind %q is not evidence or comment", kind), Hint: annotationKindHint}
}

// parseAnnotationRef returns ref as an annotation id prefix, lowercased: hex digits and dashes,
// starting with a hex digit.
func parseAnnotationRef(ref string) (string, error) {
	prefix, ok := clikit.ParseIDPrefix(ref)
	if !ok {
		return "", &clikit.Error{Kind: clikit.KindUsage, Message: fmt.Sprintf("%q is not an annotation id", ref), Hint: listAnnotationsHint}
	}

	return prefix, nil
}

func checkAnnotationRef(ref string) error {
	_, err := parseAnnotationRef(ref)
	return err
}

// findAnnotation returns the annotation whose id is, or uniquely starts with, prefix, in any
// timeline.
func findAnnotation(ctx context.Context, s clikit.Records, prefix string) (timeline.Annotation, error) {
	annotations, err := s.AnnotationsByIDPrefix(ctx, prefix, clikit.ShownMatches+1)
	switch {
	case err != nil:
		return timeline.Annotation{}, err
	case len(annotations) == 1:
		return annotations[0], nil
	case len(annotations) == 0:
		return timeline.Annotation{}, annotationNotFoundError("no annotation has an id starting "+prefix, nil)
	}

	var rows []string
	for _, a := range annotations[:min(len(annotations), clikit.ShownMatches)] {
		rows = append(rows, fmt.Sprintf("%s  %s", a.ID, a.Kind))
	}

	return timeline.Annotation{}, clikit.AmbiguousIDError("annotation", prefix, rows, len(annotations) > clikit.ShownMatches)
}

func annotationNotFoundError(message string, err error) *clikit.Error {
	return &clikit.Error{Kind: clikit.KindAnnotationNotFound, Message: message, Hint: listAnnotationsHint, Err: err}
}

// annotationFields are the source and URL an annotation's fields give it, each nil when not
// given.
type annotationFields struct {
	source, url *string
}

func readAnnotationFields(in clikit.FieldValues) annotationFields {
	return annotationFields{source: in.Value("source"), url: in.Value("url")}
}

// annotationTarget names the event e in t, or t itself when e is nil.
func annotationTarget(t timeline.Timeline, e *timeline.Event) string {
	if e == nil {
		return fmt.Sprintf("timeline `%s`", t.Name)
	}

	return fmt.Sprintf("event #%d in timeline `%s`", e.Seq, t.Name)
}
