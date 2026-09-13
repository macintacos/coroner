package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

const zoneHint = "Use an IANA time zone, such as America/New_York, or local."

// timeLocation returns the zone naive time input is read in: tz, the --tz flag (nil when not
// given), else the config file's defaultTimezone, else local. Either may say "local".
func timeLocation(tz *string, defaultTimezone string, local *time.Location) (*time.Location, error) {
	name, source := defaultTimezone, "the config file's default_timezone"
	if tz != nil {
		name, source = *tz, "--tz"
	}

	if name == "local" {
		return local, nil
	}
	// LoadLocation reads "" as UTC.
	if name == "" {
		return nil, &Error{Kind: KindUsage, Message: source + " must not be empty", Hint: zoneHint}
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, &Error{
			Kind: KindUsage, Message: fmt.Sprintf("unknown time zone %q in %s", name, source), Hint: zoneHint, Err: err,
		}
	}

	return loc, nil
}

// resolveTime resolves input, the value of flag, as of now in loc; nil input is now, to the
// second. A rejected input returns invalid_time with the parser's hint.
func resolveTime(flag string, input *string, now time.Time, loc *time.Location) (timeparse.Resolved, error) {
	if input == nil {
		local := now.In(loc)
		_, offset := local.Zone()
		return timeparse.Resolved{
			Instant: timeline.PrecisionSecond.Truncate(local), Offset: &offset, Precision: timeline.PrecisionSecond,
		}, nil
	}

	r, err := timeparse.Parse(*input, now, loc)
	if rejected, ok := errors.AsType[*timeparse.ParseError](err); ok {
		return r, &Error{Kind: KindInvalidTime, Message: flag + ": " + rejected.Error(), Hint: rejected.Hint, Err: err}
	}

	return r, err
}

// formatResolved renders r's instant in UTC with the fractional digits its precision states,
// then its offset and wall clock in loc, labelled local when loc is local.
func formatResolved(r timeparse.Resolved, loc, local *time.Location) string {
	label := loc.String()
	if loc == local {
		label = "local"
	}
	mark := ""
	if r.Approximate {
		mark = "~"
	}
	wall := r.Instant.In(loc)

	return fmt.Sprintf("%s%s (%s %s: %s)", mark, r.Instant.UTC().Format("2006-01-02T15:04:05"+fraction(r.Precision)+"Z07:00"),
		wall.Format("-07:00"), label, wall.Format(time.TimeOnly))
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
