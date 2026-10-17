package timeline_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/macintacos/coroner/internal/timeline"
)

// Plain tests, not Ginkgo specs: rapid keys its repro hint and fail files on the test name.
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

// instant draws an instant in years 0000–9999 UTC, in a random fixed offset.
func instant() *rapid.Generator[time.Time] {
	// One second inside each end, so near's ±1s step keeps four-digit years.
	first := time.Date(0, time.January, 1, 0, 0, 1, 0, time.UTC).Unix()
	last := time.Date(9999, time.December, 31, 23, 59, 58, 0, time.UTC).Unix()

	return rapid.Custom(func(t *rapid.T) time.Time {
		sec := rapid.Int64Range(first, last).Draw(t, "sec")
		return time.Unix(sec, fraction().Draw(t, "nsec")).In(zone().Draw(t, "zone"))
	})
}

// near draws an instant in a's second or one either side, so fractions often decide the order.
func near(a time.Time) *rapid.Generator[time.Time] {
	return rapid.Custom(func(t *rapid.T) time.Time {
		sec := a.Unix() + rapid.Int64Range(-1, 1).Draw(t, "step")
		return time.Unix(sec, fraction().Draw(t, "nsec")).In(zone().Draw(t, "zone"))
	})
}

// fraction draws nanoseconds at a random precision: uniform nanoseconds almost never end in
// zero, so a formatter that trims zeros would pass.
func fraction() *rapid.Generator[int64] {
	return rapid.Custom(func(t *rapid.T) int64 {
		unit := rapid.SampledFrom([]int64{1, 10, 100, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9}).Draw(t, "unit")
		return rapid.Int64Range(0, int64(time.Second)/unit-1).Draw(t, "units") * unit
	})
}

func zone() *rapid.Generator[*time.Location] {
	return rapid.Custom(func(t *rapid.T) *time.Location {
		offset := rapid.IntRange(-14*3600, 14*3600).Draw(t, "offset")
		return time.FixedZone(fmt.Sprintf("UTC%+ds", offset), offset)
	})
}
