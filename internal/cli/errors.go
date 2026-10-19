package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	issuesURL    = "https://github.com/macintacos/coroner/issues"
	internalHint = "Re-run with --debug for details, or report the bug at " + issuesURL
)

// Kind names a class of failure. Scripts branch on it through the exit code and the JSON
// error envelope, so a kind keeps its exit code; STABILITY.md says when a kind itself may
// change.
type Kind string

// The error kinds, grouped by exit code.
const (
	KindInternal Kind = "internal"

	KindUsage       Kind = "usage"
	KindInvalidTime Kind = "invalid_time"
	KindInvalidName Kind = "invalid_name"
	// KindTimelineRequired means neither -t, CORONER_TIMELINE nor the current timeline
	// selects a timeline.
	KindTimelineRequired Kind = "timeline_required"
	// KindAmbiguousID means an ID prefix matches more than one record.
	KindAmbiguousID Kind = "ambiguous_id"
	// KindInvalidInput means records a command reads, such as an import's, cannot be used.
	KindInvalidInput Kind = "invalid_input"

	KindTimelineNotFound   Kind = "timeline_not_found"
	KindEventNotFound      Kind = "event_not_found"
	KindAnnotationNotFound Kind = "annotation_not_found"
	KindSourceNotFound     Kind = "source_not_found"
	KindAttachmentNotFound Kind = "attachment_not_found"
	KindLinkNotFound       Kind = "link_not_found"
	KindOverlayNotFound    Kind = "overlay_not_found"

	KindTimelineExists Kind = "timeline_exists"
	KindSourceExists   Kind = "source_exists"
	KindLinkExists     Kind = "link_exists"
	KindOverlayExists  Kind = "overlay_exists"
	// KindIDConflict means a record with that ID already exists with different content.
	KindIDConflict Kind = "id_conflict"
	// KindFileExists means the file a command would write is already there.
	KindFileExists Kind = "file_exists"

	KindDBBusy       Kind = "db_busy"
	KindDBUnreadable Kind = "db_unreadable"
	// KindDBTooNew means the database's schema is newer than this build knows.
	KindDBTooNew Kind = "db_too_new"

	KindInterrupted Kind = "interrupted"
)

// ExitCode returns the process exit code for kind. An unknown kind exits 1, as internal.
func (kind Kind) ExitCode() int {
	code, ok := exitCodes()[kind]
	if !ok {
		return 1
	}

	return code
}

// exitCodes maps every kind to its exit code. The exit-codes help topic must list the same.
func exitCodes() map[Kind]int {
	return map[Kind]int{
		KindInternal: 1,

		KindUsage:            2,
		KindInvalidTime:      2,
		KindInvalidName:      2,
		KindTimelineRequired: 2,
		KindAmbiguousID:      2,
		KindInvalidInput:     2,

		KindTimelineNotFound:   3,
		KindEventNotFound:      3,
		KindAnnotationNotFound: 3,
		KindSourceNotFound:     3,
		KindAttachmentNotFound: 3,
		KindLinkNotFound:       3,
		KindOverlayNotFound:    3,

		KindTimelineExists: 4,
		KindSourceExists:   4,
		KindIDConflict:     4,
		KindFileExists:     4,
		KindLinkExists:     4,
		KindOverlayExists:  4,

		KindDBBusy:       5,
		KindDBUnreadable: 5,
		KindDBTooNew:     5,

		KindInterrupted: 130,
	}
}

// Error is how a command reports a failure: Message says what went wrong and Hint how to
// fix it. Err is the cause, logged under --debug and never printed otherwise. Message and
// Hint are for people and may change; Kind is what scripts branch on.
type Error struct {
	Kind          Kind
	Message, Hint string
	Err           error
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func usageError(cmd *cobra.Command, err error) *Error {
	return &Error{
		Kind:    KindUsage,
		Message: strings.TrimSpace(err.Error()),
		Hint:    fmt.Sprintf("Run '%s --help' for usage.", cmd.CommandPath()),
		Err:     err,
	}
}

// argsError is usageError with the usage line in its hint, as cobra's message about a wrong
// number of arguments never names the arguments to pass.
func argsError(cmd *cobra.Command, err error) *Error {
	failure := usageError(cmd, err)
	failure.Hint = fmt.Sprintf("Usage: %s\nRun '%s --help' for more.", cmd.UseLine(), cmd.CommandPath())
	return failure
}

// asInternal passes nil and a kinded *Error through and makes any other error an internal
// one.
func asInternal(err error) error {
	if err == nil {
		return nil
	}

	typed, ok := errors.AsType[*Error](err)
	if ok && typed.Kind != "" {
		return err
	}

	return &Error{Kind: KindInternal, Message: err.Error(), Hint: internalHint, Err: err}
}

// errorStyle is how a failure is presented on Err.
type errorStyle struct {
	color, machine bool
}

// errorStyleFor reads the global flags that shape error output straight from app.Args,
// because cobra may have failed before parsing them. It also reports whether to log debug
// detail.
func errorStyleFor(app App) (errorStyle, bool) {
	flags := globalFlags()
	flags.ParseErrorsAllowlist.UnknownFlags = true
	flags.SetOutput(io.Discard)
	// ponytail: a subcommand flag whose value starts with "--" (--text --json) reads here as
	// the next flag; parse with the failing command's own flag set if that ever matters.
	_ = flags.Parse(app.Args)

	chosen, _ := flags.GetString("format")
	asJSON, _ := flags.GetBool("json")
	noColor, debug := readSwitches(flags, app.LookupEnv)

	style := errorStyle{
		color:   colorEnabled(app.ErrIsTTY, noColor, app.LookupEnv),
		machine: asJSON || format(chosen).machine(),
	}

	return style, debug
}

// readSwitches reports whether color is off and debug is on: --no-color and --debug when
// given, else their environment variables.
func readSwitches(flags *pflag.FlagSet, lookupEnv func(string) (string, bool)) (bool, bool) {
	noColor := flagOrEnv(flags, "no-color", envSet(lookupEnv, "NO_COLOR") || envSet(lookupEnv, "CORONER_NO_COLOR"))
	return noColor, flagOrEnv(flags, "debug", envTrue(lookupEnv, "CORONER_DEBUG"))
}

// flagOrEnv returns the boolean flag name when it was given, and fromEnv otherwise.
func flagOrEnv(flags *pflag.FlagSet, name string, fromEnv bool) bool {
	if !flags.Changed(name) {
		return fromEnv
	}

	value, _ := flags.GetBool(name)
	return value
}

// colorEnabled reports whether a stream may carry color.
func colorEnabled(isTTY, noColor bool, lookupEnv func(string) (string, bool)) bool {
	term, _ := lookupEnv("TERM")
	return isTTY && !noColor && term != "dumb"
}

// envSet reports whether the environment variable key is set and non-empty.
func envSet(lookupEnv func(string) (string, bool), key string) bool {
	value, _ := lookupEnv(key)
	return value != ""
}

// envTrue reports whether the environment variable key is set to a value other than empty or
// a false value: 0, f, false, no or off, in any case.
func envTrue(lookupEnv func(string) (string, bool), key string) bool {
	value, _ := lookupEnv(key)
	value = strings.ToLower(value)
	if value == "no" || value == "off" {
		return false
	}
	on, err := strconv.ParseBool(value)
	return on || (err != nil && value != "")
}

// writeError prints e with its hint as the last line, followed under a machine format by
// the one-line JSON error envelope.
func writeError(w io.Writer, e *Error, style errorStyle) error {
	label := "Error:"
	if style.color {
		label = "\x1b[31mError:\x1b[0m"
	}

	text := label + " " + e.Message + "\n"
	if e.Hint != "" {
		text += e.Hint + "\n"
	}

	_, err := io.WriteString(w, text)
	if err != nil || !style.machine {
		return err
	}

	return writeEnvelope(w, e)
}

type envelope struct {
	Error envelopeError `json:"error"`
}

type envelopeError struct {
	Kind    Kind   `json:"kind"`
	Message string `json:"message"`
	// nil encodes as null: the envelope always carries every key.
	Hint *string `json:"hint"`
}

func writeEnvelope(w io.Writer, e *Error) error {
	body := envelopeError{Kind: e.Kind, Message: e.Message}
	if e.Hint != "" {
		body.Hint = &e.Hint
	}

	return writeJSON(w, false, envelope{Error: body})
}
