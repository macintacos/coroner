package clikit

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
	"github.com/macintacos/coroner/internal/timetext"
)

// ZoneHint is the hint of an error that rejects a time zone.
const ZoneHint = "Use an IANA time zone, such as America/New_York, or local."

// Zone is a time zone and the label the echo gives it: local, or its IANA name.
type Zone struct {
	Loc   *time.Location
	Label string
}

const tzUsage = "Time `zone` for this invocation (default: config default_timezone, then UTC)"

// TypedTime is a time typed into flag, and what it resolved to in zone.
type TypedTime struct {
	Flag, Input string
	Zone        Zone
	Resolved    timeparse.Resolved
}

// LogTypedTime logs t at debug. Call it only where a time flag is parsed for the last time, never
// from ResolveTime, which every form keystroke and a flags-only add's check also run.
func LogTypedTime(logger *slog.Logger, t TypedTime) {
	logger.Debug("time input resolved", "flag", t.Flag, "input", t.Input, "zone", t.Zone.Label,
		"instant", t.Resolved.Instant, "precision", t.Resolved.Precision)
}

// TzField is --tz, which root registers, as the field of a command that reads time input.
func TzField() Field {
	return Field{
		Name: "tz", Usage: tzUsage, Inherited: true,
		Check: func(tz string, env CheckEnv) error {
			_, err := TimeZone(&tz, "", env.Now.Location())
			return err
		},
	}
}

// CheckAt resolves value as --at, as of env.
func CheckAt(value string, env CheckEnv) error {
	_, err := ResolveAt(&value, env.Now, env.Zone.Loc)
	return err
}

// ResolveAt resolves input as --at, as ResolveTime does, and also rejects an offset more than
// a day from UTC, which an event cannot store, and input that is not UTF-8.
func ResolveAt(input *string, now time.Time, loc *time.Location) (timeparse.Resolved, error) {
	if input != nil {
		err := CheckUTF8("--at", *input)
		if err != nil {
			return timeparse.Resolved{}, err
		}
	}
	r, err := ResolveTime("--at", input, now, loc)
	if err != nil {
		return r, err
	}
	offsetErr := timeline.ValidateOccurredOffset(r.Offset)
	if offsetErr != nil {
		return r, &Error{Kind: KindOf(timeline.RuleOffsetRange), Message: "--at: " + offsetErr.Error(), Hint: "Use an offset from -24:00 to +24:00."}
	}

	return r, nil
}

// ResolveEnded resolves input as --ended, as of env, rejecting an approximate time with hint.
func ResolveEnded(input string, env CheckEnv, hint string) (timeparse.Resolved, error) {
	r, err := ResolveTime("--ended", &input, env.Now, env.Zone.Loc)
	if err == nil && r.Approximate {
		return r, &Error{Kind: KindInvalidTime, Message: "--ended must not be approximate", Hint: hint}
	}

	return r, err
}

// TimeZone returns the zone naive time input is read in: tz, the --tz flag (nil when not
// given), else the config file's defaultTimezone, which config.Resolve defaults to UTC.
// Either may say local, in any case, for the zone local holds, or UTC, in any case.
func TimeZone(tz *string, defaultTimezone string, local *time.Location) (Zone, error) {
	name, source := defaultTimezone, "the config file's default_timezone"
	if tz != nil {
		name, source = *tz, "--tz"
	}

	// LoadLocation reads "Local" as the process's time.Local, not local.
	if strings.EqualFold(name, "local") {
		return Zone{Loc: local, Label: "local"}, nil
	}
	// LoadLocation finds "utc" only on a case-insensitive filesystem, as its own location.
	if strings.EqualFold(name, "UTC") {
		return Zone{Loc: time.UTC, Label: "UTC"}, nil
	}
	// LoadLocation reads "" as UTC.
	if name == "" {
		return Zone{}, &Error{Kind: KindUsage, Message: source + " must not be empty", Hint: ZoneHint}
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return Zone{}, &Error{
			Kind: KindUsage, Message: fmt.Sprintf("unknown time zone %q in %s", name, source), Hint: ZoneHint, Err: err,
		}
	}

	return Zone{Loc: loc, Label: loc.String()}, nil
}

// ResolveTime resolves input, the value of flag, as of now in loc; nil input is now, to the
// second. A rejected input, or one outside the years an event stores, returns invalid_time.
func ResolveTime(flag string, input *string, now time.Time, loc *time.Location) (timeparse.Resolved, error) {
	if input == nil {
		wall := now.In(loc)
		return timeparse.Resolved{
			Instant:   timeline.PrecisionSecond.Truncate(wall),
			Offset:    timeparse.OffsetIn(wall, loc),
			Precision: timeline.PrecisionSecond,
		}, nil
	}

	r, err := timeparse.Parse(*input, now, loc)
	if rejected, ok := errors.AsType[*timeparse.ParseError](err); ok {
		return r, &Error{Kind: KindInvalidTime, Message: flag + ": " + rejected.Error(), Hint: rejected.Hint, Err: err}
	}
	if err != nil {
		return r, err
	}
	rangeErr := timeline.ValidateInstantRange(flag, flag, r.Instant)
	if p, ok := FirstProblem(rangeErr); ok {
		return r, &Error{
			Kind:    KindOf(p.Rule),
			Message: fmt.Sprintf("%s: time %q is in the year %d in UTC", flag, *input, r.Instant.UTC().Year()),
			Hint:    "Use a year from 0000 to 9999, in UTC.", Err: rangeErr,
		}
	}

	return r, nil
}

// TimePreview renders a resolved time as the echo prints it, with its precision, or a rejected
// input's message and hint; another error renders as its own text.
func TimePreview(r timeparse.Resolved, err error, z Zone) string {
	if rejected, ok := errors.AsType[*Error](err); ok {
		return rejected.Message + "\n" + rejected.Hint
	}
	if err != nil {
		return err.Error()
	}
	preview := FormatResolved(r, z) + ", " + string(r.Precision) + " precision"
	if r.Approximate {
		preview += ", approximate"
	}

	return preview
}

// FormatResolved renders r's instant in UTC with the fractional digits its precision states,
// then, unless z is UTC, its offset, label and wall clock in z.
func FormatResolved(r timeparse.Resolved, z Zone) string {
	mark := ""
	if r.Approximate {
		mark = "~"
	}
	instant := mark + r.Instant.UTC().Format("2006-01-02T15:04:05"+fraction(r.Precision)+"Z")
	if z.Loc == time.UTC {
		return instant
	}
	wall := r.Instant.In(z.Loc)

	return fmt.Sprintf("%s (%s %s: %s)", instant, wall.Format("-07:00"), z.Label, wall.Format(time.TimeOnly))
}

// OccurredText renders e's occurred at in z to its precision, a day-precision event in its own
// offset when its window does not line up in z, marked ~ when approximate.
func OccurredText(e timeline.Event, z Zone) string {
	if e.Precision == timeline.PrecisionDay {
		z = Zone{Loc: timetext.Location(e, z.Loc)}
	}

	if e.Approximate {
		return "~" + DisplayTime(e.OccurredAt, e.Precision, z)
	}

	return DisplayTime(e.OccurredAt, e.Precision, z)
}

// EndedText renders e's ended at as the wall clock in z, or - when it has none.
func EndedText(e timeline.Event, z Zone) string {
	if e.EndedAt == nil {
		return "-"
	}

	return e.EndedAt.In(z.Loc).Format(time.RFC3339Nano)
}

// DisplayTime renders t as the wall clock in z, with the fractional digits precision states
// and z's offset.
func DisplayTime(t time.Time, precision timeline.Precision, z Zone) string {
	return t.In(z.Loc).Format("2006-01-02T15:04:05" + fraction(precision) + "Z07:00")
}

// fraction returns the layout of the fractional seconds precision states.
func fraction(precision timeline.Precision) string {
	switch precision {
	case timeline.PrecisionMillisecond:
		return ".000"
	case timeline.PrecisionMicrosecond:
		return ".000000"
	case timeline.PrecisionNanosecond:
		return ".000000000"
	case timeline.PrecisionDay, timeline.PrecisionHour, timeline.PrecisionMinute, timeline.PrecisionSecond:
	}

	return ""
}
