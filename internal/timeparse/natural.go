package timeparse

import (
	"time"

	dateparser "github.com/markusmobius/go-dateparser"
	"github.com/markusmobius/go-dateparser/date"

	"github.com/macintacos/coroner/internal/timeline"
)

// parseNatural reads natural language through go-dateparser, preferring the past. Unlike
// the other layers its results may change when the library does.
func parseNatural(text string, now time.Time, loc *time.Location) (Resolved, error) {
	got, err := dateparse(text, now.In(loc))
	if err != nil {
		return Resolved{}, err
	}

	precision, err := periodPrecision(got.Period)
	if err != nil {
		return Resolved{}, err
	}

	// go-dateparser returns a naive input in now's location and a stated zone as a fixed
	// zone.
	if got.Time.Location() == loc {
		err = checkWallClock(text, now, loc)
	} else {
		// go-dateparser dates a zoned time from now's calendar and checks it is past as
		// though now were UTC.
		//
		// ponytail: it moves a day back at most once, so a late time far west of UTC, such
		// as "23:30 -0700" early in the UTC day, can still resolve to the future.
		got, err = dateparse(text, now.UTC())
	}

	if err != nil {
		return Resolved{}, err
	}

	return resolve(got.Time, naturalOffset(got.Time, loc), precision), nil
}

func dateparse(text string, now time.Time) (date.Date, error) {
	got, err := dateparser.Parse(&dateparser.Configuration{
		Languages:           []string{"en"},
		CurrentTime:         now,
		PreferredDateSource: dateparser.Past,
		ReturnTimeAsPeriod:  true,
	}, text)
	if err != nil {
		return date.Date{}, ErrUnrecognized
	}

	return got, nil
}

// checkWallClock rejects a naive input whose wall time a DST change in loc skips or
// repeats. go-dateparser moves a skipped wall time without an error, so the input is parsed
// again on a clock with no DST, which keeps the wall time as typed.
//
// ponytail: a relative input ("1 hour ago") landing in a skipped or repeated hour is
// rejected though its instant is exact; tell durations apart if that matters.
func checkWallClock(text string, now time.Time, loc *time.Location) error {
	typed, err := dateparse(text, wallClock(now.In(loc)))
	if err != nil {
		return err
	}

	_, err = resolveWall(wallClock(typed.Time), loc)

	return err
}

func periodPrecision(period date.Period) (timeline.Precision, error) {
	switch period {
	case date.Second:
		return timeline.PrecisionSecond, nil
	case date.Minute:
		return timeline.PrecisionMinute, nil
	case date.Hour:
		return timeline.PrecisionHour, nil
	case date.Day:
		return timeline.PrecisionDay, nil
	case date.Month, date.Year:
		return "", &ParseError{Err: ErrTooCoarse, Hint: "name a day, such as 2026-09-01"}
	case date.None:
	}

	return "", ErrUnrecognized
}

// naturalOffset is nil for a stated UTC, which says nothing about the local offset.
func naturalOffset(t time.Time, loc *time.Location) *int {
	name, offset := t.Zone()
	if t.Location() != loc && (name == "UTC" || name == "Z") {
		return nil
	}

	return &offset
}
