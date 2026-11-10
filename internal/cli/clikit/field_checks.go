package clikit

import (
	"cmp"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/timeline"
)

const clearHint = "Pass none to clear it, or leave it out."

// TextArg is a command's free-text argument: an event's text or an annotation's body.
type TextArg struct {
	// what names it in messages, as in "event text".
	what string
	// field is its key in a record's validation problems.
	field   string
	command string
	// pipeExample shows - reading it from stdin.
	pipeExample string
	quoteHint   string
}

// EventTextInput returns event add's text argument.
func EventTextInput() TextArg {
	return TextArg{
		what: "event text", field: "text", command: "event add",
		pipeExample: "pbpaste | coroner event add -",
		quoteHint:   "Quote text that has spaces: coroner event add \"Primary stepped down\"",
	}
}

// AnnotationBodyInput returns annotation add's body argument.
func AnnotationBodyInput() TextArg {
	return TextArg{
		what: "annotation body", field: "body", command: "annotation add",
		pipeExample: "pbpaste | coroner annotation add - --kind evidence",
		quoteHint:   "Quote a body that has spaces, or pass - to read it from stdin.",
	}
}

// Args rejects more than one argument, which is most often text left unquoted.
func (t TextArg) Args(_ *cobra.Command, args []string) error {
	if len(args) <= 1 {
		return nil
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("%s takes the %s as one argument, got %d", t.command, t.what, len(args)),
		Hint:    t.quoteHint,
	}
}

// Read returns the text value gives, reading in when that is -. Stdin loses one trailing
// newline and is otherwise kept verbatim; it is never read from a terminal.
func (t TextArg) Read(value string, in io.Reader, inIsTTY bool) (string, error) {
	if value != "-" {
		return value, t.Check(value)
	}
	if inIsTTY {
		return "", &Error{
			Kind:    KindUsage,
			Message: "- reads the " + t.what + " from stdin, which is a terminal",
			Hint:    "Pipe it in, as in: " + t.pipeExample,
		}
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read the %s from stdin: %w", t.what, err)
	}
	text := strings.TrimSuffix(string(data), "\n")

	return text, t.Check(text)
}

// CheckOrStdin checks text as typed, where - stands for text read from stdin later.
func (t TextArg) CheckOrStdin(text string) error {
	if text == "-" {
		return nil
	}

	return t.Check(text)
}

// Check rejects text the record cannot hold, with a hint for this argument.
func (t TextArg) Check(text string) error {
	err := timeline.ValidateText(t.field, text)
	p, ok := FirstProblem(err)
	switch {
	case !ok:
		return nil
	case p.Rule == timeline.RuleBlank:
		return &Error{Kind: KindUsage, Message: t.what + " must not be blank", Hint: "Pass non-blank text, or - to read it from stdin."}
	case p.Rule == timeline.RuleNotUTF8:
		return &Error{
			Kind: KindUsage, Message: t.what + " must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8",
		}
	}

	return RecordError(err, "")
}

// NewSourceID returns a new id for source, or "" when no source was given.
func NewSourceID(source *string, newID func() (uuid.UUID, error)) (string, error) {
	if source == nil {
		return "", nil
	}

	id, err := newID()
	return id.String(), err
}

func checkNotBlank(flag, value string) error {
	if strings.TrimSpace(value) != "" {
		return nil
	}

	return blankError(flag, "Leave "+flag+" out instead.")
}

func blankError(flag, hint string) *Error {
	return &Error{Kind: KindUsage, Message: flag + " must not be blank", Hint: hint}
}

// CheckUTF8 returns usage when value of flag is not UTF-8.
func CheckUTF8(flag, value string) error {
	if timeline.ValidateUTF8(flag, value) == nil {
		return nil
	}

	return &Error{Kind: KindUsage, Message: flag + " must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8"}
}

// Utf8Only returns a field check that rejects a value of flag that is not UTF-8.
func Utf8Only(flag string) func(string, CheckEnv) error {
	return WithoutEnv(func(value string) error {
		return CheckUTF8(flag, value)
	})
}

// FreeText returns a field check that rejects a blank value of flag, or one that is not UTF-8.
func FreeText(flag string) func(string, CheckEnv) error {
	return WithoutEnv(func(value string) error {
		err := checkNotBlank(flag, value)
		if err != nil {
			return err
		}

		return CheckUTF8(flag, value)
	})
}

// ClearableFreeText returns FreeText's check for an edit, which also accepts none to clear the
// field.
func ClearableFreeText(flag string) func(string, CheckEnv) error {
	return func(value string, env CheckEnv) error {
		if value == ClearKeyword {
			return nil
		}
		if strings.TrimSpace(value) == "" {
			return blankError(flag, clearHint)
		}

		return FreeText(flag)(value, env)
	}
}

// CheckEditedSource accepts none, which clears the source, and otherwise a source name.
func CheckEditedSource(source string) error {
	if source == ClearKeyword {
		return nil
	}
	if strings.TrimSpace(source) == "" {
		return blankError("--source", clearHint)
	}

	return CheckSource(source)
}

// CheckSource rejects a source name that is not a slug, suggesting the nearest one.
func CheckSource(source string) error {
	return checkSlug(source, timeline.ValidateSourceName, timeline.SuggestSourceName)
}

// CheckTag rejects a tag that is not a slug, suggesting the nearest one.
func CheckTag(tag string) error {
	return checkSlug(tag, timeline.ValidateTag, timeline.SuggestTag)
}

func checkSlug(slug string, validate func(string) error, suggest func(string) string) error {
	err := validate(slug)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_', ':', '/' and '-'."
	if slug == ClearKeyword {
		hint = "none clears an event's source in event edit, so no source can be called it; pick another name."
	} else if suggestion := suggest(slug); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`?", suggestion)
	}

	return &Error{Kind: KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// DefaultAuthor returns flag, else configured, else USER, or nil when all are empty, rejecting
// a defaulted author that is not UTF-8.
func DefaultAuthor(flag *string, configured string, lookupEnv func(string) (string, bool)) (*string, error) {
	if flag != nil {
		return flag, nil
	}

	user, _ := lookupEnv("USER")
	author := cmp.Or(configured, user)
	if timeline.ValidateUTF8("author", author) != nil {
		return nil, &Error{
			Kind:    KindUsage,
			Message: "the default author, from the config file's author or USER, must be UTF-8",
			Hint:    "Pass --author, or fix the config author or USER.",
		}
	}

	var defaulted *string
	if author != "" {
		defaulted = &author
	}

	return defaulted, nil
}

// OrDash returns s's value, or - when s is nil.
func OrDash(s *string) string {
	if s == nil {
		return "-"
	}

	return *s
}
