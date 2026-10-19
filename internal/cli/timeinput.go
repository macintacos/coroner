package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
	"github.com/macintacos/coroner/internal/timetext"
)

const zoneHint = "Use an IANA time zone, such as America/New_York, or local."

// zone is a time zone and the label the echo gives it: local, or its IANA name.
type zone struct {
	loc   *time.Location
	label string
}

const tzUsage = "Time `zone` for this invocation (default: config default_timezone, then TZ)"

// tzField is --tz, which root registers, as the field of a command that reads time input.
func tzField() field {
	return field{
		name: "tz", usage: tzUsage, inherited: true,
		check: func(tz string, env checkEnv) error {
			_, err := timeZone(&tz, "", env.now.Location())
			return err
		},
	}
}

// checkAt resolves value as --at, as of env.
func checkAt(value string, env checkEnv) error {
	_, err := resolveAt(&value, env.now, env.zone.loc)
	return err
}

// resolveAt resolves input as --at, as resolveTime does, and also rejects an offset more than
// a day from UTC, which an event cannot store, and input that is not UTF-8.
func resolveAt(input *string, now time.Time, loc *time.Location) (timeparse.Resolved, error) {
	if input != nil {
		err := checkUTF8("--at", *input)
		if err != nil {
			return timeparse.Resolved{}, err
		}
	}
	r, err := resolveTime("--at", input, now, loc)
	if err != nil {
		return r, err
	}
	offsetErr := timeline.ValidateOccurredOffset(r.Offset)
	if offsetErr != nil {
		return r, &Error{Kind: kindOf(timeline.RuleOffsetRange), Message: "--at: " + offsetErr.Error(), Hint: "Use an offset from -24:00 to +24:00."}
	}

	return r, nil
}

// resolveEnded resolves input as --ended, as of env, rejecting an approximate time with hint.
func resolveEnded(input string, env checkEnv, hint string) (timeparse.Resolved, error) {
	r, err := resolveTime("--ended", &input, env.now, env.zone.loc)
	if err == nil && r.Approximate {
		return r, &Error{Kind: KindInvalidTime, Message: "--ended must not be approximate", Hint: hint}
	}

	return r, err
}

// timeZone returns the zone naive time input is read in: tz, the --tz flag (nil when not
// given), else the config file's defaultTimezone, which config.Resolve defaults to local.
// Either may say local, in any case, for the local zone.
func timeZone(tz *string, defaultTimezone string, local *time.Location) (zone, error) {
	name, source := defaultTimezone, "the config file's default_timezone"
	if tz != nil {
		name, source = *tz, "--tz"
	}

	// LoadLocation reads "Local" as the process's time.Local, not local.
	if strings.EqualFold(name, "local") {
		return zone{loc: local, label: "local"}, nil
	}
	// LoadLocation reads "" as UTC.
	if name == "" {
		return zone{}, &Error{Kind: KindUsage, Message: source + " must not be empty", Hint: zoneHint}
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return zone{}, &Error{
			Kind: KindUsage, Message: fmt.Sprintf("unknown time zone %q in %s", name, source), Hint: zoneHint, Err: err,
		}
	}

	return zone{loc: loc, label: loc.String()}, nil
}

// resolveTime resolves input, the value of flag, as of now in loc; nil input is now, to the
// second. A rejected input, or one outside the years an event stores, returns invalid_time.
func resolveTime(flag string, input *string, now time.Time, loc *time.Location) (timeparse.Resolved, error) {
	if input == nil {
		wall := now.In(loc)
		_, offset := wall.Zone()
		return timeparse.Resolved{
			Instant: timeline.PrecisionSecond.Truncate(wall), Offset: &offset, Precision: timeline.PrecisionSecond,
		}, nil
	}

	r, err := timeparse.Parse(*input, now, loc)
	if rejected, ok := errors.AsType[*timeparse.ParseError](err); ok {
		return r, &Error{Kind: KindInvalidTime, Message: flag + ": " + rejected.Error(), Hint: rejected.Hint, Err: err}
	}
	if year := r.Instant.UTC().Year(); err == nil && (year < 0 || year > 9999) {
		return r, &Error{
			Kind:    KindInvalidTime,
			Message: fmt.Sprintf("%s: time %q is in the year %d in UTC", flag, *input, year),
			Hint:    "Use a year from 0000 to 9999, in UTC.",
		}
	}

	return r, err
}

// timePreview renders a resolved time as the echo prints it, with its precision, or a rejected
// input's message and hint; another error renders as its own text.
func timePreview(r timeparse.Resolved, err error, z zone) string {
	if rejected, ok := errors.AsType[*Error](err); ok {
		return rejected.Message + "\n" + rejected.Hint
	}
	if err != nil {
		return err.Error()
	}
	preview := formatResolved(r, z) + ", " + string(r.Precision) + " precision"
	if r.Approximate {
		preview += ", approximate"
	}

	return preview
}

// formatResolved renders r's instant in UTC with the fractional digits its precision states,
// then its offset, label and wall clock in z.
func formatResolved(r timeparse.Resolved, z zone) string {
	mark := ""
	if r.Approximate {
		mark = "~"
	}
	wall := r.Instant.In(z.loc)

	return fmt.Sprintf("%s%s (%s %s: %s)", mark, r.Instant.UTC().Format("2006-01-02T15:04:05"+fraction(r.Precision)+"Z"),
		wall.Format("-07:00"), z.label, wall.Format(time.TimeOnly))
}

// occurredText renders e's occurred at in z to its precision, marked ~ when approximate.
func occurredText(e timeline.Event, z zone) string {
	if e.Precision == timeline.PrecisionDay {
		z = zone{loc: timetext.Location(e, z.loc), label: z.label}
	}

	if e.Approximate {
		return "~" + displayTime(e.OccurredAt, e.Precision, z)
	}

	return displayTime(e.OccurredAt, e.Precision, z)
}

// displayTime renders t as the wall clock in z, with the fractional digits precision states
// and z's offset.
func displayTime(t time.Time, precision timeline.Precision, z zone) string {
	return t.In(z.loc).Format("2006-01-02T15:04:05" + fraction(precision) + "Z07:00")
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
