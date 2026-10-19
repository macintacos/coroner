package cli

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

// textArg is a command's free-text argument: an event's text or an annotation's body.
type textArg struct {
	// what names it in messages, as in "event text".
	what string
	// field is its key in a record's validation problems.
	field   string
	command string
	// pipeExample shows - reading it from stdin.
	pipeExample string
	quoteHint   string
}

func eventTextInput() textArg {
	return textArg{
		what: "event text", field: "text", command: "event add",
		pipeExample: "pbpaste | coroner event add -",
		quoteHint:   "Quote text that has spaces: coroner event add \"Primary stepped down\"",
	}
}

func annotationBodyInput() textArg {
	return textArg{
		what: "annotation body", field: "body", command: "annotation add",
		pipeExample: "pbpaste | coroner annotation add - --kind evidence",
		quoteHint:   "Quote a body that has spaces, or pass - to read it from stdin.",
	}
}

// args rejects more than one argument, which is most often text left unquoted.
func (t textArg) args(_ *cobra.Command, args []string) error {
	if len(args) <= 1 {
		return nil
	}

	return &Error{
		Kind:    KindUsage,
		Message: fmt.Sprintf("%s takes the %s as one argument, got %d", t.command, t.what, len(args)),
		Hint:    t.quoteHint,
	}
}

// read returns the text value gives, reading in when that is -. Stdin loses one trailing
// newline and is otherwise kept verbatim; it is never read from a terminal.
func (t textArg) read(value string, in io.Reader, inIsTTY bool) (string, error) {
	if value != "-" {
		return value, t.check(value)
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

	return text, t.check(text)
}

// checkOrStdin checks text as typed, where - stands for text read from stdin later.
func (t textArg) checkOrStdin(text string) error {
	if text == "-" {
		return nil
	}

	return t.check(text)
}

func (t textArg) check(text string) error {
	err := timeline.ValidateText(t.field, text)
	p, ok := firstProblem(err)
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

	return recordError(err, "")
}

// newSourceID returns a new id for source, or "" when no source was given.
func newSourceID(source *string, newID func() (uuid.UUID, error)) (string, error) {
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

// checkUTF8 returns usage when value of flag is not UTF-8.
func checkUTF8(flag, value string) error {
	if timeline.ValidateUTF8(flag, value) == nil {
		return nil
	}

	return &Error{Kind: KindUsage, Message: flag + " must be UTF-8", Hint: "Convert it first, as with: iconv -f <encoding> -t UTF-8"}
}

// utf8Only returns a field check that rejects a value of flag that is not UTF-8.
func utf8Only(flag string) func(string, checkEnv) error {
	return withoutEnv(func(value string) error {
		return checkUTF8(flag, value)
	})
}

// freeText returns a field check that rejects a blank value of flag, or one that is not UTF-8.
func freeText(flag string) func(string, checkEnv) error {
	return withoutEnv(func(value string) error {
		err := checkNotBlank(flag, value)
		if err != nil {
			return err
		}

		return checkUTF8(flag, value)
	})
}

// clearableFreeText returns freeText's check for an edit, which also accepts none to clear the
// field.
func clearableFreeText(flag string) func(string, checkEnv) error {
	return func(value string, env checkEnv) error {
		if value == clearKeyword {
			return nil
		}
		if strings.TrimSpace(value) == "" {
			return blankError(flag, clearHint)
		}

		return freeText(flag)(value, env)
	}
}

// checkEditedSource accepts none, which clears the source, and otherwise a source name.
func checkEditedSource(source string) error {
	if source == clearKeyword {
		return nil
	}
	if strings.TrimSpace(source) == "" {
		return blankError("--source", clearHint)
	}

	return checkSource(source)
}

func checkSource(source string) error {
	return checkSlug(source, timeline.ValidateSourceName, timeline.SuggestSourceName)
}

func checkTag(tag string) error {
	return checkSlug(tag, timeline.ValidateTag, timeline.SuggestTag)
}

func checkSlug(slug string, validate func(string) error, suggest func(string) string) error {
	err := validate(slug)
	if err == nil {
		return nil
	}

	hint := "Use lowercase letters, digits, '.', '_', ':', '/' and '-'."
	if slug == clearKeyword {
		hint = "none clears an event's source in event edit, so no source can be called it; pick another name."
	} else if suggestion := suggest(slug); suggestion != "" {
		hint = fmt.Sprintf("Did you mean `%s`?", suggestion)
	}

	return &Error{Kind: KindInvalidName, Message: err.Error(), Hint: hint, Err: err}
}

// defaultAuthor returns flag, else configured, else USER, or nil when all are empty, rejecting
// a defaulted author that is not UTF-8.
func defaultAuthor(flag *string, configured string, lookupEnv func(string) (string, bool)) (*string, error) {
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

func orDash(s *string) string {
	if s == nil {
		return "-"
	}

	return *s
}
