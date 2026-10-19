package cli

import (
	"errors"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

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

	DescribeTable("timeZone reads naive input in --tz, else the config default, labelled local for the local zone",
		func(tz *string, defaultTimezone, wantZone, wantLabel string) {
			got, err := timeZone(tz, defaultTimezone, local)

			Expect(err).NotTo(HaveOccurred())
			Expect(got.loc.String()).To(Equal(wantZone))
			Expect(got.label).To(Equal(wantLabel))
		},
		Entry("local when the config default says so", nil, "local", "Test/Local", "local"),
		Entry("the config default when --tz is not given", nil, "Asia/Tokyo", "Asia/Tokyo", "Asia/Tokyo"),
		Entry("--tz over the config default", new("Europe/Berlin"), "Asia/Tokyo", "Europe/Berlin", "Europe/Berlin"),
		Entry("local when --tz says so, over the config default", new("local"), "Asia/Tokyo", "Test/Local", "local"),
		Entry("local in any case", new("Local"), "Asia/Tokyo", "Test/Local", "local"),
	)

	DescribeTable("timeZone rejects a zone it cannot load as usage, naming where the zone came from",
		func(tz *string, defaultTimezone, source string) {
			_, err := timeZone(tz, defaultTimezone, local)

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

		DescribeTable("rejects an instant outside the years 0000 to 9999 in UTC as invalid_time, naming the flag",
			func(input string) {
				_, err := resolveTime("--at", &input, now, time.UTC)

				Expect(err).To(SatisfyAll(
					HaveField("Kind", KindInvalidTime),
					MatchError(ContainSubstring("--at")),
					HaveField("Hint", ContainSubstring("0000 to 9999"))))
			},
			Entry("before 0000", "0000-01-01T00:00:00+01:00"),
			Entry("after 9999", "9999-12-31T23:00:00-01:00"),
		)
	})

	DescribeTable("formatResolved shows the UTC instant to its precision, then the wall clock in the zone",
		func(r timeparse.Resolved, zoneName, want string) {
			z, err := timeZone(&zoneName, "local", local)
			Expect(err).NotTo(HaveOccurred())

			Expect(formatResolved(r, z)).To(Equal(want))
		},
		Entry("milliseconds, in local",
			resolved(412_000_000, timeline.PrecisionMillisecond, false), "local",
			"2026-09-11T03:14:07.412Z (-03:00 local: 00:14:07)"),
		Entry("seconds, in a named zone",
			resolved(0, timeline.PrecisionSecond, false), "America/New_York",
			"2026-09-11T03:14:07Z (-04:00 America/New_York: 23:14:07)"),
		Entry("microseconds",
			resolved(412_000, timeline.PrecisionMicrosecond, false), "local",
			"2026-09-11T03:14:07.000412Z (-03:00 local: 00:14:07)"),
		Entry("nanoseconds",
			resolved(412, timeline.PrecisionNanosecond, false), "UTC",
			"2026-09-11T03:14:07.000000412Z (+00:00 UTC: 03:14:07)"),
		Entry("an approximate time, marked with ~",
			resolved(0, timeline.PrecisionSecond, true), "local",
			"~2026-09-11T03:14:07Z (-03:00 local: 00:14:07)"),
	)

	DescribeTable("occurredText dates a day-precision event in its own offset, and any other in the zone",
		func(occurred string, precision timeline.Precision, offset int, want string) {
			at, err := time.Parse(time.RFC3339, occurred)
			Expect(err).NotTo(HaveOccurred())
			e := timeline.Event{OccurredAt: at, Precision: precision, OccurredOffset: &offset}
			newYork, err := time.LoadLocation("America/New_York")
			Expect(err).NotTo(HaveOccurred())

			Expect(occurredText(e, zone{loc: newYork, label: "America/New_York"})).To(Equal(want))
		},
		Entry("a day recorded in UTC", "2026-09-11T00:00:00Z", timeline.PrecisionDay, 0,
			"2026-09-11T00:00:00Z"),
		Entry("a day recorded at -05:00", "2026-09-11T00:00:00-05:00", timeline.PrecisionDay, -5*3600,
			"2026-09-11T00:00:00-05:00"),
		Entry("a second, in the zone", "2026-09-11T03:14:07Z", timeline.PrecisionSecond, 0,
			"2026-09-10T23:14:07-04:00"),
	)

	DescribeTable("event add's time previews show what a time resolves to in the zone, or why its check rejects it",
		func(name, input string, want types.GomegaMatcher) {
			z, err := timeZone(new("America/New_York"), "local", local)
			Expect(err).NotTo(HaveOccurred())
			def := eventAddDefinition()
			found := def.fields[slices.IndexFunc(def.fields, func(f field) bool { return f.name == name })]

			// testNow is 2026-09-12T17:30:00-04:00.
			Expect(found.preview(input, checkEnv{now: testNow(), zone: z})).To(want)
		},
		Entry("RFC 3339", "at", "2026-09-11T03:14:07Z",
			Equal("2026-09-11T03:14:07Z (-04:00 America/New_York: 23:14:07), second precision")),
		Entry("a naive wall clock, read in the zone", "at", "2026-09-11 03:14",
			Equal("2026-09-11T07:14:00Z (-04:00 America/New_York: 03:14:00), minute precision")),
		Entry("an approximate relative time", "at", "~ yesterday 23:02",
			Equal("~2026-09-12T03:02:00Z (-04:00 America/New_York: 23:02:00), minute precision, approximate")),
		Entry("epoch seconds", "at", "1789096447",
			Equal("2026-09-11T03:14:07Z (-04:00 America/New_York: 23:14:07), second precision")),
		Entry("no --at, as now", "at", "",
			Equal("2026-09-12T21:30:00Z (-04:00 America/New_York: 17:30:00), second precision")),
		Entry("nothing for no --ended", "ended", "", BeEmpty()),
		Entry("a DST gap's rejection, then its hint naming the offsets", "ended", "2026-03-08 02:30",
			MatchRegexp(`^--ended: .+\n.*-05:00`)),
		Entry("an approximate --ended's rejection, then its hint", "ended", "~ 2026-09-11 14:20",
			Equal("--ended must not be approximate\n"+addedEndedHint)),
	)

	It("timePreview shows an error that carries no hint as its text", func() {
		Expect(timePreview(timeparse.Resolved{}, errors.New("disk on fire"), zone{loc: time.UTC, label: "UTC"})).
			To(Equal("disk on fire"))
	})

	DescribeTable("displayTime shows an instant's wall clock in the zone, to its precision, with the zone's offset",
		func(nanos int, precision timeline.Precision, zoneName, want string) {
			z, err := timeZone(&zoneName, "local", local)
			Expect(err).NotTo(HaveOccurred())

			Expect(displayTime(resolved(nanos, precision, false).Instant, precision, z)).To(Equal(want))
		},
		Entry("seconds, in local", 0, timeline.PrecisionSecond, "local", "2026-09-11T00:14:07-03:00"),
		Entry("milliseconds, across midnight in a named zone",
			412_000_000, timeline.PrecisionMillisecond, "America/New_York", "2026-09-10T23:14:07.412-04:00"),
		Entry("nanoseconds, in UTC", 412, timeline.PrecisionNanosecond, "UTC", "2026-09-11T03:14:07.000000412Z"),
	)
})

// resolved is 2026-09-11T03:14:07Z plus nanos, at precision.
func resolved(nanos int, precision timeline.Precision, approximate bool) timeparse.Resolved {
	instant := time.Date(2026, time.September, 11, 3, 14, 7, nanos, time.UTC)
	return timeparse.Resolved{Instant: instant, Precision: precision, Approximate: approximate}
}
