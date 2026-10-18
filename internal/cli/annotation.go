package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	annotationKindHint  = "Pass --kind evidence for raw material such as a log line, or --kind comment for commentary."
	listAnnotationsHint = "Run 'coroner annotation list' to see a timeline's annotations and their ids."
	// shownAnnotations is how many annotations an ambiguous id prefix lists.
	shownAnnotations = 10
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
		// Help and the unknown-verb error print under any format.
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		RunE:        runNoun,
	}

	cmd.AddCommand(
		newAnnotationAddCommand(ctx, app),
		newAnnotationListCommand(ctx, app),
		newAnnotationEditCommand(ctx, app),
		newAnnotationDeleteCommand(ctx, app),
	)

	return cmd
}

// annotationJSON is the canonical annotation object's keys, every one always present.
type annotationJSON struct {
	ID         string  `json:"id"`
	TimelineID string  `json:"timeline_id"`
	EventID    *string `json:"event_id"`
	Kind       string  `json:"kind"`
	Body       string  `json:"body"`
	Author     *string `json:"author"`
	Source     *string `json:"source"`
	URL        *string `json:"url"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func newAnnotationJSON(a timeline.Annotation) annotationJSON {
	return annotationJSON{
		ID:         a.ID,
		TimelineID: a.TimelineID,
		EventID:    a.EventID,
		Kind:       string(a.Kind),
		Body:       a.Body,
		Author:     a.Author,
		Source:     a.Source,
		URL:        a.URL,
		CreatedAt:  timeline.FormatInstant(a.CreatedAt),
		UpdatedAt:  timeline.FormatInstant(a.UpdatedAt),
	}
}

type annotationListJSON struct {
	Annotations []annotationJSON `json:"annotations"`
}

func newAnnotationListJSON(annotations []timeline.Annotation) annotationListJSON {
	listed := make([]annotationJSON, 0, len(annotations))
	for _, a := range annotations {
		listed = append(listed, newAnnotationJSON(a))
	}

	return annotationListJSON{Annotations: listed}
}

// printAnnotation prints a when o asks for JSON; text output leaves it to the command's message.
func printAnnotation(o output, a timeline.Annotation) error {
	if o.format != formatJSON {
		return nil
	}

	return o.json(newAnnotationJSON(a))
}

// previewAnnotation prints a as a dry run would store it: its JSON object, or heading and a
// description.
func previewAnnotation(o output, a timeline.Annotation, heading string) error {
	if o.format == formatJSON {
		return o.json(newAnnotationJSON(a))
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

// checkAnnotationKind returns kind as an annotation kind, or usage naming the kinds there are.
func checkAnnotationKind(kind string) (timeline.AnnotationKind, error) {
	switch k := timeline.AnnotationKind(kind); k {
	case timeline.AnnotationEvidence, timeline.AnnotationComment:
		return k, nil
	}

	return "", &Error{Kind: KindUsage, Message: fmt.Sprintf("--kind %q is not evidence or comment", kind), Hint: annotationKindHint}
}

func checkAnnotationKindName(kind string) error {
	_, err := checkAnnotationKind(kind)
	return err
}

// annotationBody returns the body value gives, reading in when that is -. Stdin loses one
// trailing newline and is otherwise kept verbatim; it is never read from a terminal.
func annotationBody(value string, in io.Reader, inIsTTY bool) (string, error) {
	body := value
	if value == "-" {
		if inIsTTY {
			return "", &Error{
				Kind:    KindUsage,
				Message: "- reads the annotation body from stdin, which is a terminal",
				Hint:    "Pipe the body in, as in: pbpaste | coroner annotation add - --kind evidence",
			}
		}
		data, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("read the annotation body from stdin: %w", err)
		}
		body = strings.TrimSuffix(string(data), "\n")
	}

	return body, checkBody(body)
}

// checkBodyOrStdin checks a body as typed, where - stands for a body read from stdin later.
func checkBodyOrStdin(body string) error {
	if body == "-" {
		return nil
	}

	return checkBody(body)
}

func checkBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return &Error{Kind: KindUsage, Message: "annotation body must not be blank", Hint: "Pass non-blank text, or - to read it from stdin."}
	}
	// JSON would replace invalid bytes, so an export could never match what is stored.
	if !utf8.ValidString(body) {
		return &Error{
			Kind: KindUsage, Message: "annotation body must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8",
		}
	}

	return nil
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

// findEventIn returns the event ref names, which must be in t: an event in another timeline
// is event_not_found.
func findEventIn(
	ctx context.Context, s *store.Store, t timeline.Timeline, ref eventRef, selection timelineSelection,
) (timeline.Event, error) {
	// findEvent rejects -t with any ref but a bare seq; here -t already selected t, checked below.
	if ref.timeline != "" || ref.idPrefix != "" {
		selection.flag = nil
	}
	e, eventTimeline, err := findEvent(ctx, s, ref, selection)
	if err != nil || e.TimelineID == t.ID {
		return e, err
	}

	return timeline.Event{}, &Error{
		Kind:    KindEventNotFound,
		Message: fmt.Sprintf("event %s#%d is not in timeline `%s`", eventTimeline.Name, e.Seq, t.Name),
		Hint:    "Pass -t " + eventTimeline.Name + " to act on its own timeline.",
	}
}

// parseAnnotationRef returns ref as an annotation id prefix, lowercased: hex digits and dashes,
// starting with a hex digit.
func parseAnnotationRef(ref string) (string, error) {
	prefix := strings.ToLower(ref)
	if prefix == "" || !strings.ContainsRune("0123456789abcdef", rune(prefix[0])) ||
		strings.Trim(prefix, "0123456789abcdef-") != "" {
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
func findAnnotation(ctx context.Context, s *store.Store, prefix string) (timeline.Annotation, error) {
	annotations, err := s.AnnotationsByIDPrefix(ctx, prefix, shownAnnotations+1)
	switch {
	case err != nil:
		return timeline.Annotation{}, err
	case len(annotations) == 1:
		return annotations[0], nil
	case len(annotations) == 0:
		return timeline.Annotation{}, annotationNotFoundError("no annotation has an id starting "+prefix, nil)
	}

	lines := make([]string, 0, shownAnnotations+1)
	for _, a := range annotations[:min(len(annotations), shownAnnotations)] {
		lines = append(lines, fmt.Sprintf("  %s  %s", a.ID, a.Kind))
	}
	if len(annotations) > shownAnnotations {
		lines = append(lines, "  and more")
	}

	return timeline.Annotation{}, &Error{
		Kind:    KindAmbiguousID,
		Message: "more than one annotation has an id starting " + prefix,
		Hint:    "Pass more of the id:\n" + strings.Join(lines, "\n"),
	}
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
