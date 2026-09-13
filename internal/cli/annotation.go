package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/macintacos/coroner/internal/timeline"
)

const kindHint = "Pass --kind evidence for raw material such as a log line, or --kind comment for commentary."

func newAnnotationCommand(ctx context.Context, app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "annotation",
		Short:   "Attach evidence and comments to events",
		GroupID: coreCommands,
		Long: "An annotation is evidence, raw material kept verbatim such as a log line, or a comment,\n" +
			"attached to an event or to the whole timeline. It has no time of its own in the\n" +
			"chronology.",
		Example: "  $ pbpaste | coroner annotation add - -e 12 --kind evidence --source mongod",
		// Help and the unknown-verb error print under any format.
		Annotations: map[string]string{formatsAnnotation: "text,json,jsonl"},
		RunE:        runNoun,
	}

	cmd.AddCommand(newAnnotationAddCommand(ctx, app))

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

// checkKind returns kind as an annotation kind, or usage naming the kinds there are.
func checkKind(kind string) (timeline.AnnotationKind, error) {
	switch k := timeline.AnnotationKind(kind); k {
	case timeline.AnnotationEvidence, timeline.AnnotationComment:
		return k, nil
	}

	return "", &Error{Kind: KindUsage, Message: fmt.Sprintf("--kind %q is not evidence or comment", kind), Hint: kindHint}
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

	if strings.TrimSpace(body) == "" {
		return "", &Error{Kind: KindUsage, Message: "annotation body must not be blank", Hint: "Pass non-blank text, or - to read it from stdin."}
	}
	// JSON would replace invalid bytes, so an export could never match what is stored.
	if !utf8.ValidString(body) {
		return "", &Error{
			Kind: KindUsage, Message: "annotation body must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8",
		}
	}

	return body, nil
}

// annotationFields are the source and URL the flags give an annotation, each nil when not
// given.
type annotationFields struct {
	source, url *string
}

func readAnnotationFields(flags *pflag.FlagSet) (annotationFields, error) {
	f := annotationFields{source: changedString(flags, "source"), url: changedString(flags, "url")}
	err := checkNotBlank("--url", f.url)
	if err != nil {
		return f, err
	}

	return f, checkSlugs(f.source, nil)
}

// annotationTarget describes where a is attached, as in "event #12 in timeline `x`".
func annotationTarget(t timeline.Timeline, eventSeq int64) string {
	if eventSeq == 0 {
		return fmt.Sprintf("timeline `%s`", t.Name)
	}

	return fmt.Sprintf("event #%d in timeline `%s`", eventSeq, t.Name)
}
