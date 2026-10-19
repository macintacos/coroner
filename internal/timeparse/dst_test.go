package timeparse

import (
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"pgregory.net/rapid"
)

// A plain test, not a Ginkgo spec: rapid keys its repro hint and fail files on the test name.
func TestResolveWallFindsTheInstantOrRejectsARepeatedWallClock(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		zone := rapid.SampledFrom([]string{
			"UTC", "Asia/Kolkata", "America/New_York", "Europe/London", "America/Santiago", "Australia/Lord_Howe",
		}).Draw(t, "zone")
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}

		first := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
		last := time.Date(2037, time.December, 31, 0, 0, 0, 0, time.UTC).Unix()
		instant := time.Unix(rapid.Int64Range(first, last).Draw(t, "sec"), 0)
		// Uniform instants almost never land by a DST change, so most draws move next to one.
		if _, end := instant.In(loc).ZoneBounds(); !end.IsZero() && rapid.Bool().Draw(t, "near change") {
			instant = end.Add(time.Duration(rapid.IntRange(-2*60*60, 2*60*60).Draw(t, "from change")) * time.Second)
		}

		got, err := resolveWall(wallClock(instant.In(loc)), loc)

		switch {
		case errors.Is(err, ErrDSTGap):
			t.Fatalf("resolveWall(%v in %s) = %v; the wall clock is an instant's", instant.In(loc), zone, err)
		case repeated(instant, loc) != errors.Is(err, ErrDSTFold):
			t.Fatalf("resolveWall(%v in %s) = %v; repeated is %v", instant.In(loc), zone, err, repeated(instant, loc))
		case err == nil && !got.Equal(instant):
			t.Fatalf("resolveWall(%v in %s) = %v", instant.In(loc), zone, got)
		}
	})
}

// repeated reports whether another instant within two hours reads the same wall clock in
// loc. Every DST shift in the sampled zones is a multiple of 30 minutes and at most an hour.
func repeated(instant time.Time, loc *time.Location) bool {
	const layout = "2006-01-02T15:04:05.999999999"

	for step := -4; step <= 4; step++ {
		other := instant.Add(time.Duration(step) * 30 * time.Minute)
		if step != 0 && other.In(loc).Format(layout) == instant.In(loc).Format(layout) {
			return true
		}
	}

	return false
}
