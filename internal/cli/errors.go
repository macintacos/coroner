package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

const issuesURL = "https://github.com/macintacos/coroner/issues"

// Kind names a class of failure. Scripts branch on it through the exit code and the JSON
// error envelope, so a kind's meaning and exit code never change.
type Kind string

// The error kinds, grouped by exit code.
const (
	KindInternal Kind = "internal"

	KindUsage            Kind = "usage"
	KindInvalidTime      Kind = "invalid_time"
	KindInvalidName      Kind = "invalid_name"
	KindTimelineRequired Kind = "timeline_required"
	KindAmbiguousID      Kind = "ambiguous_id"

	KindTimelineNotFound Kind = "timeline_not_found"
	KindEventNotFound    Kind = "event_not_found"

	KindTimelineExists Kind = "timeline_exists"
	KindIDConflict     Kind = "id_conflict"

	KindDBBusy       Kind = "db_busy"
	KindDBUnreadable Kind = "db_unreadable"
	KindDBTooNew     Kind = "db_too_new"

	KindInterrupted Kind = "interrupted"
)

// ExitCode returns the process exit code for kind.
func (kind Kind) ExitCode() int {
	switch kind {
	case KindInternal:
		return 1
	case KindUsage, KindInvalidTime, KindInvalidName, KindTimelineRequired, KindAmbiguousID:
		return 2
	case KindTimelineNotFound, KindEventNotFound:
		return 3
	case KindTimelineExists, KindIDConflict:
		return 4
	case KindDBBusy, KindDBUnreadable, KindDBTooNew:
		return 5
	case KindInterrupted:
		return 130
	}

	return 1
}

// Error is how a command reports a failure: Message says what went wrong and Hint how to
// fix it. Only Kind is stable; the text may change.
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

// asInternal passes nil and *Error through and makes any other error an internal one.
func asInternal(err error) error {
	if err == nil {
		return nil
	}

	if _, ok := errors.AsType[*Error](err); ok {
		return err
	}

	return &Error{
		Kind:    KindInternal,
		Message: err.Error(),
		Hint:    "Re-run with --debug for details, or report the bug at " + issuesURL,
		Err:     err,
	}
}

// errorStyle is how a failure is presented on Err.
type errorStyle struct {
	color, debug, machine bool
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
	Kind    Kind    `json:"kind"`
	Message string  `json:"message"`
	Hint    *string `json:"hint"`
}

func writeEnvelope(w io.Writer, e *Error) error {
	body := envelopeError{Kind: e.Kind, Message: e.Message}
	if e.Hint != "" {
		body.Hint = &e.Hint
	}

	return writeJSON(w, false, envelope{Error: body})
}
