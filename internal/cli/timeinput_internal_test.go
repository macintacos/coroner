package cli

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/timeparse"
)

var _ = Describe("time input", func() {
	// A named fixed zone, so a result read from the real local zone could not pass for it.
	local := time.FixedZone("Test/Local", -3*60*60)
	loadZone := func(name string) *time.Location {
		GinkgoHelper()
		loc, err := time.LoadLocation(name)
		Expect(err).NotTo(HaveOccurred())
		return loc
	}

	DescribeTable("timeLocation reads naive input in --tz, then the config default, then local",
		func(tz *string, defaultTimezone, want string) {
			got, err := timeLocation(tz, defaultTimezone, local)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.String()).To(Equal(want))
		},
		Entry("local when neither names a zone", nil, "local", "Test/Local"),
		Entry("the config default over local", nil, "Asia/Tokyo", "Asia/Tokyo"),
		Entry("--tz over the config default", new("Europe/Berlin"), "Asia/Tokyo", "Europe/Berlin"),
		Entry("local when --tz says so, over the config default", new("local"), "Asia/Tokyo", "Test/Local"),
	)

	DescribeTable("timeLocation rejects a zone it cannot load as usage, naming where the zone came from",
		func(tz *string, defaultTimezone, source string) {
			_, err := timeLocation(tz, defaultTimezone, local)

			Expect(err).To(SatisfyAll(HaveField("Kind", KindUsage), MatchError(ContainSubstring(source))))
		},
		Entry("an empty --tz", new(""), "local", "--tz"),
		Entry("an unknown --tz", new("Mars/Olympus"), "local", "--tz"),
		Entry("an unknown config default", nil, "Mars/Olympus", "default_timezone"),
	)

	Describe("resolveTime", func() {
		now := time.Date(2026, time.September, 13, 1, 30, 5, 123456789, time.UTC)

		It("resolves no input to now at second precision, in loc's offset", func() {
			got, err := resolveTime("--at", nil, now, loadZone("Asia/Tokyo"))

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Instant).To(BeTemporally("==", time.Date(2026, time.September, 13, 1, 30, 5, 0, time.UTC)))
			Expect(got).To(SatisfyAll(
				HaveField("Offset", HaveValue(Equal(9*60*60))),
				HaveField("Precision", timeline.PrecisionSecond),
				HaveField("Approximate", BeFalse())))
		})

		It("keeps the offset an input states over loc's", func() {
			got, err := resolveTime("--at", new("2026-09-11T03:14:07+02:00"), now, loadZone("Asia/Tokyo"))

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Offset).To(HaveValue(Equal(2 * 60 * 60)))
		})

		It("reads a naive wall clock in loc", func() {
			got, err := resolveTime("--at", new("2026-09-11 03:14"), now, loadZone("Asia/Tokyo"))

			Expect(err).NotTo(HaveOccurred())
			Expect(got.Instant).To(BeTemporally("==", time.Date(2026, time.September, 10, 18, 14, 0, 0, time.UTC)))
		})

		It("rejects a wall clock a DST change skips as invalid_time, naming the flag and the offsets", func() {
			_, err := resolveTime("--ended", new("2026-03-08 02:30"), now, loadZone("America/New_York"))

			Expect(err).To(SatisfyAll(
				HaveField("Kind", KindInvalidTime),
				MatchError(ContainSubstring("--ended")),
				HaveField("Hint", ContainSubstring("-05:00"))))
		})
	})

	DescribeTable("formatResolved shows the UTC instant to its precision, then the wall clock in loc",
		func(r timeparse.Resolved, zoneName, want string) {
			loc := local
			if zoneName != "" {
				loc = loadZone(zoneName)
			}

			Expect(formatResolved(r, loc, local)).To(Equal(want))
		},
		Entry("milliseconds, in local",
			resolved(412_000_000, timeline.PrecisionMillisecond, false), "",
			"2026-09-11T03:14:07.412Z (-03:00 local: 00:14:07)"),
		Entry("seconds, in a named zone",
			resolved(0, timeline.PrecisionSecond, false), "America/New_York",
			"2026-09-11T03:14:07Z (-04:00 America/New_York: 23:14:07)"),
		Entry("microseconds",
			resolved(412_000, timeline.PrecisionMicrosecond, false), "",
			"2026-09-11T03:14:07.000412Z (-03:00 local: 00:14:07)"),
		Entry("nanoseconds",
			resolved(412, timeline.PrecisionNanosecond, false), "UTC",
			"2026-09-11T03:14:07.000000412Z (+00:00 UTC: 03:14:07)"),
		Entry("an approximate time, marked with ~",
			resolved(0, timeline.PrecisionSecond, true), "",
			"~2026-09-11T03:14:07Z (-03:00 local: 00:14:07)"),
	)
})

// resolved is 2026-09-11T03:14:07Z plus nanos, at precision.
func resolved(nanos int, precision timeline.Precision, approximate bool) timeparse.Resolved {
	instant := time.Date(2026, time.September, 11, 3, 14, 7, nanos, time.UTC)
	return timeparse.Resolved{Instant: instant, Precision: precision, Approximate: approximate}
}
