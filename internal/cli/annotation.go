package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	annotationKindHint  = "Pass --kind evidence for raw material such as a log line, or --kind comment for commentary."
	listAnnotationsHint = "Run 'coroner annotation list' to see a timeline's annotations and their ids."
)

func newAnnotationCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "annotation",
		Short:   "Attach evidence and comments to events and timelines",
		GroupID: coreCommands,
		Long: "An annotation is evidence, raw material kept verbatim such as a log line, or a comment,\n" +
			"attached to an event or to the whole timeline. It has no time of its own in the\n" +
			"chronology.",
		Example: "  $ pbpaste | coroner annotation add - -e 12 --kind evidence --source mongod",
		RunE:    runNoun,
	}

	cmd.AddCommand(
		newAnnotationAddCommand(ctx, app),
		newAnnotationListCommand(ctx, app),
		newAnnotationEditCommand(ctx, app),
		newAnnotationDeleteCommand(ctx, app),
	)

	return cmd
}

// annotationJSON is the export annotation with its output-only attachments.
type annotationJSON struct {
	export.Annotation

	// Never nil, so no attachments encode as [] rather than null.
	Attachments []attachmentJSON `json:"attachments"`
}

func newAnnotationJSON(a timeline.Annotation, attachments []attachmentJSON) annotationJSON {
	return annotationJSON{Annotation: export.NewAnnotation(a), Attachments: append([]attachmentJSON{}, attachments...)}
}

type annotationListJSON struct {
	Annotations []annotationJSON `json:"annotations"`
}

// newAnnotationListJSON returns annotations' objects, each with its attachments from attached,
// keyed by annotation id.
func newAnnotationListJSON(annotations []timeline.Annotation, attached map[string][]attachmentJSON) annotationListJSON {
	listed := make([]annotationJSON, 0, len(annotations))
	for _, a := range annotations {
		listed = append(listed, newAnnotationJSON(a, attached[a.ID]))
	}

	return annotationListJSON{Annotations: listed}
}

// printAnnotation prints a with attachments when o asks for JSON; text output leaves it to the
// command's message.
func printAnnotation(o output, a timeline.Annotation, attachments []attachmentJSON) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newAnnotationJSON(a, attachments))
}

// previewAnnotation prints a as a dry run would store it: its JSON object with attachments, or
// heading and a description.
func previewAnnotation(o output, a timeline.Annotation, attachments []attachmentJSON, heading string) error {
	if o.format == formatJSON {
		return o.json(newAnnotationJSON(a, attachments))
	}

	var b strings.Builder
	b.WriteString(heading + "\n")
	writeField(&b, "Kind:", string(a.Kind))
	writeField(&b, "Source:", orDash(a.Source))
	writeField(&b, "URL:", orDash(a.URL))
	writeField(&b, "Author:", orDash(a.Author))
	writeField(&b, "Body:", a.Body)

	_, err := io.WriteString(o.out, b.String())
	return err
}

// checkAnnotationKindName returns usage naming the kinds there are unless kind is one.
func checkAnnotationKindName(kind string) error {
	if timeline.AnnotationKind(kind).Valid() {
		return nil
	}

	return &Error{Kind: KindUsage, Message: fmt.Sprintf("--kind %q is not evidence or comment", kind), Hint: annotationKindHint}
}

// eventFlag returns the event reference -e gives, or nil when -e is not given.
func eventFlag(flags *pflag.FlagSet) (*eventRef, error) {
	return optionalEventRef(changedString(flags, "event"))
}

// optionalEventRef returns the event reference value gives, or nil when value is nil.
func optionalEventRef(value *string) (*eventRef, error) {
	var ref *eventRef
	if value != nil {
		parsed, err := parseEventRef(*value)
		if err != nil {
			return nil, err
		}
		ref = &parsed
	}

	return ref, nil
}

// parseAnnotationRef returns ref as an annotation id prefix, lowercased: hex digits and dashes,
// starting with a hex digit.
func parseAnnotationRef(ref string) (string, error) {
	prefix, ok := parseIDPrefix(ref)
	if !ok {
		return "", &Error{Kind: KindUsage, Message: fmt.Sprintf("%q is not an annotation id", ref), Hint: listAnnotationsHint}
	}

	return prefix, nil
}

func checkAnnotationRef(ref string) error {
	_, err := parseAnnotationRef(ref)
	return err
}

// findAnnotation returns the annotation whose id is, or uniquely starts with, prefix, in any
// timeline.
func findAnnotation(ctx context.Context, s records, prefix string) (timeline.Annotation, error) {
	annotations, err := s.AnnotationsByIDPrefix(ctx, prefix, shownMatches+1)
	switch {
	case err != nil:
		return timeline.Annotation{}, err
	case len(annotations) == 1:
		return annotations[0], nil
	case len(annotations) == 0:
		return timeline.Annotation{}, annotationNotFoundError("no annotation has an id starting "+prefix, nil)
	}

	var rows []string
	for _, a := range annotations[:min(len(annotations), shownMatches)] {
		rows = append(rows, fmt.Sprintf("%s  %s", a.ID, a.Kind))
	}

	return timeline.Annotation{}, ambiguousIDError("annotation", prefix, rows, len(annotations) > shownMatches)
}

func annotationNotFoundError(message string, err error) *Error {
	return &Error{Kind: KindAnnotationNotFound, Message: message, Hint: listAnnotationsHint, Err: err}
}

// annotationFields are the source and URL an annotation's fields give it, each nil when not
// given.
type annotationFields struct {
	source, url *string
}

func readAnnotationFields(in fieldValues) annotationFields {
	return annotationFields{source: in.value("source"), url: in.value("url")}
}

// annotationTarget names the event e in t, or t itself when e is nil.
func annotationTarget(t timeline.Timeline, e *timeline.Event) string {
	if e == nil {
		return fmt.Sprintf("timeline `%s`", t.Name)
	}

	return fmt.Sprintf("event #%d in timeline `%s`", e.Seq, t.Name)
}
