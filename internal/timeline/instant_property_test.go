package timeline_test

import (
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/timeline"
)

func TestFormatInstantSortsChronologically(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := instant().Draw(t, "a")
		b := rapid.OneOf(instant(), near(a)).Draw(t, "b")

		fa, fb := timeline.FormatInstant(a), timeline.FormatInstant(b)
		if got, want := strings.Compare(fa, fb), a.Compare(b); got != want {
			t.Fatalf("strings.Compare(%q, %q) = %d, want %d", fa, fb, got, want)
		}
	})
}

func TestParseInstantReadsBackFormatInstant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := instant().Draw(t, "a")

		parsed, err := timeline.ParseInstant(timeline.FormatInstant(a))
		if err != nil {
			t.Fatal(err)
		}
		if !parsed.Equal(a) {
			t.Fatalf("ParseInstant(FormatInstant(%v)) = %v", a, parsed)
		}
	})
}

// instant draws an instant from 0001-01-01 to 9999-12-31 UTC, in a random fixed offset.
func instant() *rapid.Generator[time.Time] {
	first := time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	// A second short of the end, so near can step forward without reaching year 10000.
	last := time.Date(9999, time.December, 31, 23, 59, 58, 0, time.UTC).Unix()

	return rapid.Custom(func(t *rapid.T) time.Time {
		sec := rapid.Int64Range(first, last).Draw(t, "sec")
		return time.Unix(sec, fraction().Draw(t, "nsec")).In(zone().Draw(t, "zone"))
	})
}

// near draws an instant within a second of a, so fraction digits decide the order.
func near(a time.Time) *rapid.Generator[time.Time] {
	return rapid.Custom(func(t *rapid.T) time.Time {
		sec := a.Unix() + rapid.Int64Range(-1, 1).Draw(t, "sec")
		return time.Unix(sec, fraction().Draw(t, "nsec")).In(zone().Draw(t, "zone"))
	})
}

// fraction draws nanoseconds at a random precision from whole seconds to nanoseconds, so
// trailing zeros are common, as they are in real timestamps.
func fraction() *rapid.Generator[int64] {
	return rapid.Custom(func(t *rapid.T) int64 {
		unit := int64(1)
		for range rapid.IntRange(0, 9).Draw(t, "dropped digits") {
			unit *= 10
		}
		return rapid.Int64Range(0, int64(time.Second)/unit-1).Draw(t, "units") * unit
	})
}

func zone() *rapid.Generator[*time.Location] {
	return rapid.Custom(func(t *rapid.T) *time.Location {
		return time.FixedZone("", rapid.IntRange(-14*3600, 14*3600).Draw(t, "offset"))
	})
}
