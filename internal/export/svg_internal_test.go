package export

import (
	"strings"
	"time"
	"unicode/utf8"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

func at(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		panic(err)
	}

	return t
}

func instant(value string, precision timeline.Precision, source string) Event {
	e := Event{OccurredAt: Instant(at(value)), Precision: precision, Text: "event at " + value}
	if source != "" {
		e.Source = &source
	}

	return e
}

func interval(from, to, source string) Event {
	e := instant(from, timeline.PrecisionSecond, source)
	e.EndedAt = new(Instant(at(to)))

	return e
}

func threeToFour() svgWindow {
	start := at("2026-09-11T03:00:00Z")
	return svgWindow{From: start, To: start.Add(time.Hour)}
}

func marks(layout svgLayout) []svgMark {
	var all []svgMark
	for _, lane := range layout.Lanes {
		all = append(all, lane.Marks...)
	}

	return all
}

var _ = Describe("layoutSVG", func() {
	// The plot spans x 200 to 1176: 976 wide.
	It("places times proportionally across the window, and labels on the side with room", func() {
		layout := layoutSVG([]Event{
			instant("2026-09-11T03:15:00Z", timeline.PrecisionSecond, "a"),
			interval("2026-09-11T03:20:00Z", "2026-09-11T03:40:00Z", "a"),
			interval("2026-09-11T03:30:00Z", "2026-09-11T04:00:00Z", "a"),
		}, threeToFour(), time.UTC)

		got := marks(layout)
		Expect(got).To(HaveLen(3))
		point, straddling, rightBar := got[0], got[1], got[2]
		Expect(straddling.Anchor).To(Equal("start"))
		Expect(straddling.LabelX).To(BeNumerically("~", straddling.X+svgLabelGap, 0.001))
		Expect(point.X).To(BeNumerically("~", 444, 0.001))
		Expect(point.Width).To(BeZero())
		Expect(point.Anchor).To(Equal("start"))
		Expect(point.LabelX).To(BeNumerically(">", point.X))
		Expect(rightBar.X).To(BeNumerically("~", 688, 0.001))
		Expect(rightBar.Width).To(BeNumerically("~", 488, 0.001))
		Expect(rightBar.Shape).To(Equal(svgBar))
		Expect(rightBar.Anchor).To(Equal("end"))
		Expect(rightBar.LabelX).To(BeNumerically("<", rightBar.X))
	})

	It("gives each source a lane in order of first appearance, then (no source), with a row per event", func() {
		layout := layoutSVG([]Event{
			instant("2026-09-11T03:01:00Z", timeline.PrecisionSecond, "mongod"),
			instant("2026-09-11T03:02:00Z", timeline.PrecisionSecond, "slack"),
			instant("2026-09-11T03:03:00Z", timeline.PrecisionSecond, ""),
			instant("2026-09-11T03:04:00Z", timeline.PrecisionSecond, "mongod"),
		}, threeToFour(), time.UTC)

		Expect(layout.Lanes).To(HaveExactElements(
			And(HaveField("Label", "mongod"), HaveField("Color", sourcePalette()[0].Light), HaveField("Marks", HaveLen(2))),
			And(HaveField("Label", "slack"), HaveField("Color", sourcePalette()[1].Light), HaveField("Marks", HaveLen(1))),
			And(HaveField("Label", "(no source)"), HaveField("Color", svgMuted), HaveField("Marks", HaveLen(1))),
		))
		Expect(layout.Lanes[0].Marks[1].Title).To(ContainSubstring("event at 2026-09-11T03:04:00Z"))
		var previous svgLane
		for _, lane := range layout.Lanes {
			Expect(lane.Y).To(BeNumerically(">=", previous.Bottom))
			top := lane.Y
			for _, m := range lane.Marks {
				Expect(m.Y).To(BeNumerically(">", top))
				Expect(m.Y).To(BeNumerically("<", lane.Bottom))
				top = m.Y
			}
			previous = lane
		}
		Expect(layout.Height).To(BeNumerically("~", previous.Bottom, 0.001))
	})

	It("wraps the palette when there are more sources than colours", func() {
		var events []Event
		for i := range len(sourcePalette()) + 1 {
			e := instant("2026-09-11T03:00:00Z", timeline.PrecisionSecond, string(rune('a'+i)))
			e.OccurredAt = Instant(at("2026-09-11T03:00:00Z").Add(time.Duration(i) * time.Minute))
			events = append(events, e)
		}

		layout := layoutSVG(events, threeToFour(), time.UTC)

		Expect(layout.Lanes[len(sourcePalette())].Color).To(Equal(layout.Lanes[0].Color))
	})

	DescribeTable("draws a time as exact as its precision, and a guessed or coarse one as inexact, labelled on the zone's clock",
		func(e Event, shape svgShape, width float64, inexact bool, labelPrefix string) {
			got := marks(layoutSVG([]Event{e}, threeToFour(), time.UTC))

			Expect(got).To(HaveLen(1))
			Expect(got[0].Shape).To(Equal(shape))
			Expect(got[0].Width).To(BeNumerically("~", width, 0.001))
			Expect(got[0].Inexact).To(Equal(inexact))
			Expect(got[0].Label).To(HavePrefix(labelPrefix))
		},
		Entry("a second as an exact point", instant("2026-09-11T03:15:00Z", timeline.PrecisionSecond, ""), svgPoint, 0.0, false, "03:15:00 event"),
		Entry("a millisecond as an exact point", instant("2026-09-11T03:15:00.25Z", timeline.PrecisionMillisecond, ""), svgPoint, 0.0, false, "03:15:00.250 event"),
		Entry("a minute as an inexact band a minute wide", instant("2026-09-11T03:15:00Z", timeline.PrecisionMinute, ""), svgBand, 976.0/60, true, "03:15 event"),
		Entry("an hour as an inexact band the whole window wide", instant("2026-09-11T03:00:00Z", timeline.PrecisionHour, ""), svgBand, 976.0, true, "03:-- event"),
		Entry("an approximate second as an inexact point marked ~",
			func() Event {
				e := instant("2026-09-11T03:15:00Z", timeline.PrecisionSecond, "")
				e.Approximate = true
				return e
			}(), svgPoint, 0.0, true, "~03:15:00 event"),
		Entry("an interval as a bar from start to end",
			interval("2026-09-11T03:15:00Z", "2026-09-11T03:30:00Z", ""), svgBar, 244.0, false, "03:15:00–03:30:00 event"),
		Entry("a zero-length interval as a bar wide enough to see",
			interval("2026-09-11T03:15:00Z", "2026-09-11T03:15:00Z", ""), svgBar, 2.0, false, "03:15:00–03:15:00 event"),
		Entry("a millisecond interval as a bar wide enough to see",
			interval("2026-09-11T03:15:00Z", "2026-09-11T03:15:00.001Z", ""), svgBar, 2.0, false, "03:15:00–03:15:00 event"),
	)

	It("starts a day-precision band where its day starts in its own offset, labelled with that date", func() {
		e := instant("2026-09-11T05:00:00Z", timeline.PrecisionDay, "")
		e.OccurredOffset = new(-5 * 60 * 60)
		window := svgWindow{From: at("2026-09-11T00:00:00Z"), To: at("2026-09-13T00:00:00Z")}

		got := marks(layoutSVG([]Event{e}, window, time.UTC))

		Expect(got[0].X).To(BeNumerically("~", 200+976.0*5/48, 0.001))
		Expect(got[0].Width).To(BeNumerically("~", 976.0/2, 0.001))
		Expect(got[0].Label).To(HavePrefix("2026-09-11 event"))
	})

	It("cuts a long label on a rune boundary, keeping the whole text for the tooltip", func() {
		e := instant("2026-09-11T03:15:00Z", timeline.PrecisionSecond, "")
		e.Text = strings.Repeat("é", 200)

		got := marks(layoutSVG([]Event{e}, threeToFour(), time.UTC))[0]

		Expect(utf8.ValidString(got.Label)).To(BeTrue())
		Expect(utf8.RuneCountInString(got.Label)).To(Equal(svgLabelRunes))
		Expect(got.Label).To(HaveSuffix("…"))
		Expect(got.Title).To(Equal("03:15:00 " + e.Text))
	})

	It("puts a line-broken text on one line", func() {
		e := instant("2026-09-11T03:15:00Z", timeline.PrecisionSecond, "")
		e.Text = "first\r\nsecond\n"

		Expect(marks(layoutSVG([]Event{e}, threeToFour(), time.UTC))[0].Label).To(Equal("03:15:00 first second"))
	})

	DescribeTable("ticks the axis at round times on the zone's wall clock, dating the first tick and each new day",
		func(from, to, zone string, want []svgTick) {
			loc, err := time.LoadLocation(zone)
			Expect(err).NotTo(HaveOccurred())
			window := svgWindow{From: at(from), To: at(to)}

			layout := layoutSVG([]Event{instant(from, timeline.PrecisionSecond, "")}, window, loc)

			Expect(layout.Ticks).To(HaveLen(len(want)))
			for i, tick := range layout.Ticks {
				Expect(tick.Time).To(Equal(want[i].Time), "tick %d", i)
				Expect(tick.Date).To(Equal(want[i].Date), "tick %d", i)
				Expect(tick.X).To(BeNumerically("~", want[i].X, 0.01), "tick %d", i)
			}
		},
		Entry("every ten minutes in UTC", "2026-09-11T03:07:00Z", "2026-09-11T03:52:00Z", "UTC", []svgTick{
			{X: 200 + 976.0*3/45, Time: "03:10", Date: "2026-09-11"},
			{X: 200 + 976.0*13/45, Time: "03:20"},
			{X: 200 + 976.0*23/45, Time: "03:30"},
			{X: 200 + 976.0*33/45, Time: "03:40"},
			{X: 200 + 976.0*43/45, Time: "03:50"},
		}),
		Entry("the same window in New York", "2026-09-11T03:07:00Z", "2026-09-11T03:52:00Z", "America/New_York", []svgTick{
			{X: 200 + 976.0*3/45, Time: "23:10", Date: "2026-09-10"},
			{X: 200 + 976.0*13/45, Time: "23:20"},
			{X: 200 + 976.0*23/45, Time: "23:30"},
			{X: 200 + 976.0*33/45, Time: "23:40"},
			{X: 200 + 976.0*43/45, Time: "23:50"},
		}),
		Entry("across midnight", "2026-09-11T23:50:00Z", "2026-09-12T00:20:00Z", "UTC", []svgTick{
			{X: 200, Time: "23:50", Date: "2026-09-11"},
			{X: 200 + 976.0*5/30, Time: "23:55"},
			{X: 200 + 976.0*10/30, Time: "00:00", Date: "2026-09-12"},
			{X: 200 + 976.0*15/30, Time: "00:05"},
			{X: 200 + 976.0*20/30, Time: "00:10"},
			{X: 200 + 976.0*25/30, Time: "00:15"},
			{X: 200 + 976.0, Time: "00:20"},
		}),
		Entry("by the second under a minute", "2026-09-11T03:14:00Z", "2026-09-11T03:14:20Z", "UTC", []svgTick{
			{X: 200, Time: "03:14:00", Date: "2026-09-11"},
			{X: 200 + 976.0*5/20, Time: "03:14:05"},
			{X: 200 + 976.0*10/20, Time: "03:14:10"},
			{X: 200 + 976.0*15/20, Time: "03:14:15"},
			{X: 200 + 976.0, Time: "03:14:20"},
		}),
		Entry("by the day over days, from local midnight", "2026-09-10T12:00:00Z", "2026-09-14T12:00:00Z", "UTC", []svgTick{
			{X: 200 + 976.0*12/96, Date: "2026-09-11"},
			{X: 200 + 976.0*36/96, Date: "2026-09-12"},
			{X: 200 + 976.0*60/96, Date: "2026-09-13"},
			{X: 200 + 976.0*84/96, Date: "2026-09-14"},
		}),
		Entry("every four days over a month, from the first midnight", "2026-09-01T03:00:00Z", "2026-10-01T03:00:00Z", "UTC", []svgTick{
			{X: 200 + 976.0*21/720, Date: "2026-09-02"},
			{X: 200 + 976.0*117/720, Date: "2026-09-06"},
			{X: 200 + 976.0*213/720, Date: "2026-09-10"},
			{X: 200 + 976.0*309/720, Date: "2026-09-14"},
			{X: 200 + 976.0*405/720, Date: "2026-09-18"},
			{X: 200 + 976.0*501/720, Date: "2026-09-22"},
			{X: 200 + 976.0*597/720, Date: "2026-09-26"},
			{X: 200 + 976.0*693/720, Date: "2026-09-30"},
		}),
	)

	It("lays out nothing for no events", func() {
		Expect(layoutSVG(nil, svgWindow{}, time.UTC)).To(Equal(svgLayout{}))
	})
})

var _ = Describe("exportWindow", func() {
	It("spans the events' starts to the furthest their intervals and precision windows reach", func() {
		window := exportWindow(Query{}, []Event{
			instant("2026-09-11T03:00:00Z", timeline.PrecisionSecond, ""),
			interval("2026-09-11T03:10:00Z", "2026-09-11T03:40:00Z", ""),
			instant("2026-09-11T03:30:00Z", timeline.PrecisionHour, ""),
		})

		Expect(window).To(Equal(svgWindow{From: at("2026-09-11T03:00:00Z"), To: at("2026-09-11T04:30:00Z")}))
	})

	It("widens to the query's since and until", func() {
		q := Query{Since: new(Instant(at("2026-09-11T02:00:00Z"))), Until: new(Instant(at("2026-09-11T05:00:00Z")))}

		window := exportWindow(q, []Event{instant("2026-09-11T03:00:00Z", timeline.PrecisionSecond, "")})

		Expect(window).To(Equal(svgWindow{From: at("2026-09-11T02:00:00Z"), To: at("2026-09-11T05:00:00Z")}))
	})

	It("gives a single moment a minute", func() {
		window := exportWindow(Query{}, []Event{instant("2026-09-11T03:00:00Z", timeline.PrecisionSecond, "")})

		Expect(window).To(Equal(svgWindow{From: at("2026-09-11T03:00:00Z"), To: at("2026-09-11T03:01:00Z")}))
	})

	It("gives a window shorter than a minute a minute", func() {
		window := exportWindow(Query{}, []Event{
			instant("2026-09-11T03:00:00.000Z", timeline.PrecisionMillisecond, ""),
			instant("2026-09-11T03:00:00.100Z", timeline.PrecisionMillisecond, ""),
		})

		Expect(window).To(Equal(svgWindow{From: at("2026-09-11T03:00:00Z"), To: at("2026-09-11T03:01:00Z")}))
	})
})
