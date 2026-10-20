// Package timeparse resolves the time a person or agent typed into an instant, the offset
// it was read in, its precision and whether it was approximate.
package timeparse

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/macintacos/coroner/internal/timeline"
)

const (
	digits = "0123456789"
	hint   = "use RFC 3339, such as 2026-09-12T14:05:03Z"
)

// Resolved is a typed time resolved to an instant.
type Resolved struct {
	// Instant is the start of its Precision window, measured in the offset Offset states
	// (UTC when nil).
	Instant time.Time
	// Offset is seconds east of UTC; nil when the input was Z, -00:00, UTC or an epoch, or
	// was read in the UTC location, which say nothing about the local offset.
	Offset *int
	// Precision is the finest unit the input stated.
	Precision timeline.Precision
	// Approximate is true when the input began with ~ or around.
	Approximate bool
}

// Kinds of rejected input, matched with [errors.Is].
var (
	ErrUnrecognized = errors.New("not a time coroner recognizes")
	ErrDSTGap       = errors.New("that wall-clock time does not exist in the time zone")
	ErrDSTFold      = errors.New("that wall-clock time happens twice in the time zone")
	ErrTooCoarse    = errors.New("names a week, month or year, not a day")
	ErrFuture       = errors.New("names a time after now")
)

// errNoMatch is a layer declining an input so the next layer can try it. Any other error
// from a layer rejects the input.
var errNoMatch = errors.New("not this layer's format")

// ParseError is a rejected input. Err is ErrUnrecognized, ErrDSTGap, ErrDSTFold,
// ErrTooCoarse or ErrFuture; Hint says how to fix the input.
type ParseError struct {
	Input, Hint string
	Err         error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("time %q: %v", e.Input, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

type layer func(text string, now time.Time, loc *time.Location) (Resolved, error)

// Parse resolves input as of now. A leading ~ or around marks it approximate. A naive
// input is read in loc; an input that states an offset or zone is read in that. Every
// error is a *ParseError.
func Parse(input string, now time.Time, loc *time.Location) (Resolved, error) {
	text, approximate := cutApproximate(strings.TrimSpace(input))

	for _, parse := range []layer{parseEpoch, parseLayout, parseNaive, parseSyslog, parseNatural} {
		got, err := parse(text, now, loc)
		if errors.Is(err, errNoMatch) {
			continue
		}

		if err != nil {
			rejected, ok := errors.AsType[*ParseError](err)
			if !ok {
				rejected = &ParseError{Err: fmt.Errorf("%w: %w", ErrUnrecognized, err), Hint: hint}
			}

			rejected.Input = input

			return Resolved{}, rejected
		}

		got.Approximate = approximate

		return got, nil
	}

	return Resolved{}, &ParseError{Input: input, Err: ErrUnrecognized, Hint: hint}
}

func cutApproximate(text string) (string, bool) {
	const around = "around"

	if rest, ok := strings.CutPrefix(text, "~"); ok {
		return strings.TrimSpace(rest), true
	}

	if strings.IndexFunc(text, unicode.IsSpace) == len(around) && strings.EqualFold(text[:len(around)], around) {
		return strings.TrimSpace(text[len(around):]), true
	}

	return text, false
}

// parseEpoch reads an epoch, optionally after GNU date's @, whose unit its digit count
// gives: 10 seconds, 13 milliseconds, 16 microseconds or 19 nanoseconds. Epoch seconds may
// carry a fraction.
func parseEpoch(text string, _ time.Time, _ *time.Location) (Resolved, error) {
	whole, fraction, fractional := strings.Cut(strings.TrimPrefix(text, "@"), ".")
	// Four digits or fewer is a year for natural language.
	if len(whole) <= 4 || !isDigits(whole) {
		return Resolved{}, errNoMatch
	}

	if fractional && (len(whole) != 10 || len(fraction) > 9 || !isDigits(fraction)) {
		return Resolved{}, &ParseError{Err: ErrUnrecognized, Hint: "only epoch seconds take a fraction, of 1 to 9 digits"}
	}

	epoch, err := strconv.ParseInt(whole, 10, 64)

	switch len(whole) {
	case 10:
		nanos, _ := strconv.ParseInt((fraction + "000000000")[:9], 10, 64)
		return resolve(time.Unix(epoch, nanos).UTC(), nil, precisionOf(len(fraction))), nil
	case 13:
		return resolve(time.UnixMilli(epoch).UTC(), nil, timeline.PrecisionMillisecond), nil
	case 16:
		return resolve(time.UnixMicro(epoch).UTC(), nil, timeline.PrecisionMicrosecond), nil
	case 19:
		if err != nil {
			return Resolved{}, &ParseError{Err: ErrUnrecognized, Hint: "use RFC 3339 for times after 2262-04-11"}
		}

		return resolve(time.Unix(0, epoch).UTC(), nil, timeline.PrecisionNanosecond), nil
	}

	return Resolved{}, &ParseError{Err: ErrUnrecognized, Hint: "an epoch has 10, 13, 16 or 19 digits"}
}

func isDigits(text string) bool {
	return text != "" && strings.Trim(text, digits) == ""
}

// parseLayout reads the standard library layouts that state an offset. time.Parse accepts a
// fractional second after the seconds field, with a dot or a comma, even where the layout
// shows none.
func parseLayout(text string, _ time.Time, _ *time.Location) (Resolved, error) {
	// RFC 3339 allows a lowercase t and z.
	text = strings.ToUpper(text)

	for _, layout := range []string{
		time.RFC3339, "2006-01-02T15:04:05-0700", "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05-0700",
		"2006-01-02T15:04Z07:00",
	} {
		t, err := time.ParseInLocation(layout, text, time.UTC)
		if err != nil {
			continue
		}

		var offset *int
		// ParseInLocation reads +00:00 as time.UTC too, so only the text tells Z apart.
		if !strings.HasSuffix(text, "Z") && !strings.HasSuffix(text, "-00:00") && !strings.HasSuffix(text, "-0000") {
			_, seconds := t.Zone()
			offset = &seconds
		}

		return resolve(t, offset, layoutPrecision(layout, text)), nil
	}

	return Resolved{}, errNoMatch
}

// exactShape is a date and time with an optional fraction and numeric offset: the shape
// parseLayout and parseNaive claim, so a malformed one never reaches natural language.
var exactShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d+:\d+(:\d+([.,]\d*)?)?(Z|[+-][\d:]*)?$`)

// parseNaive reads a date and time with no offset as a wall clock in loc, and rejects an
// input of exactShape that neither it nor parseLayout read.
func parseNaive(text string, _ time.Time, loc *time.Location) (Resolved, error) {
	text = strings.ToUpper(text)

	for _, layout := range []string{time.DateTime, "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02T15:04"} {
		wall, err := time.ParseInLocation(layout, text, time.UTC)
		if err == nil {
			return resolveLocal(wallClock(wall), loc, layoutPrecision(layout, text))
		}
	}

	if exactShape.MatchString(text) {
		return Resolved{}, &ParseError{Err: ErrUnrecognized, Hint: hint}
	}

	return Resolved{}, errNoMatch
}

// parseSyslog reads a yearless syslog stamp as a wall clock in loc, in the latest year that
// puts it on a real date no later than now.
func parseSyslog(text string, now time.Time, loc *time.Location) (Resolved, error) {
	stamp, err := time.ParseInLocation(time.Stamp, text, time.UTC)
	// A stamp with an impossible value, such as Feb 30, is syslog's to reject.
	failed, ok := errors.AsType[*time.ParseError](err)
	if ok && strings.HasSuffix(failed.Message, "out of range") {
		return Resolved{}, &ParseError{Err: fmt.Errorf("%w: %w", ErrUnrecognized, err), Hint: hint}
	}

	if err != nil {
		return Resolved{}, errNoMatch
	}

	nowWall := wallClock(now.In(loc)).t
	for year := nowWall.Year(); ; year-- {
		wall := time.Date(year, stamp.Month(), stamp.Day(),
			stamp.Hour(), stamp.Minute(), stamp.Second(), stamp.Nanosecond(), time.UTC)
		// time.Date moves February 29 to March 1 outside a leap year.
		if wall.Day() == stamp.Day() && !wall.After(nowWall) {
			return resolveLocal(wallClock(wall), loc, precisionOf(fractionDigits(text)))
		}
	}
}

func resolveLocal(wall wallTime, loc *time.Location, precision timeline.Precision) (Resolved, error) {
	t, err := resolveWall(wall, loc)
	if err != nil {
		return Resolved{}, err
	}

	return resolve(t, OffsetIn(t, loc), precision), nil
}

// OffsetIn is the offset of t, a time read in loc, as Resolved.Offset states it: nil when loc
// is time.UTC.
func OffsetIn(t time.Time, loc *time.Location) *int {
	if loc == time.UTC {
		return nil
	}
	_, offset := t.Zone()

	return &offset
}

func layoutPrecision(layout, text string) timeline.Precision {
	if !strings.Contains(layout, ":05") {
		return timeline.PrecisionMinute
	}

	return precisionOf(fractionDigits(text))
}

// fractionDigits counts the digits after the first . or , which must follow the seconds:
// no layout has either before its seconds field.
func fractionDigits(text string) int {
	i := strings.IndexAny(text, ".,")
	if i < 0 {
		return 0
	}

	fraction := text[i+1:]

	return len(fraction) - len(strings.TrimLeft(fraction, digits))
}

func precisionOf(subsecondDigits int) timeline.Precision {
	switch {
	case subsecondDigits == 0:
		return timeline.PrecisionSecond
	case subsecondDigits <= 3:
		return timeline.PrecisionMillisecond
	case subsecondDigits <= 6:
		return timeline.PrecisionMicrosecond
	default:
		return timeline.PrecisionNanosecond
	}
}

func resolve(t time.Time, offset *int, precision timeline.Precision) Resolved {
	return Resolved{Instant: precision.Truncate(t), Offset: offset, Precision: precision}
}
