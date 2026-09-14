package export

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/macintacos/coroner/internal/timeline"
)

const (
	svgWidth  = 1200.0
	svgGutter = 200.0
	svgMargin = 24.0
	// svgPlotWidth is the width of the time axis, between the lane labels and the right margin.
	svgPlotWidth   = svgWidth - svgGutter - svgMargin
	svgAxisHeight  = 40.0
	svgRowHeight   = 22.0
	svgLanePadding = 6.0
	svgLabelGap    = 8.0
	// svgLabelRunes and svgLaneLabelRunes cut labels short, since an SVG cannot measure text;
	// the full text stays in a tooltip.
	svgLabelRunes     = 60
	svgLaneLabelRunes = 24
	svgMaxTicks       = 8
	// svgMuted colours the lane of events with no source.
	svgMuted = "#6b7280"
)

// svgWindow is the stretch of time the axis spans.
type svgWindow struct{ From, To time.Time }

// svgShape is how a mark draws an event.
type svgShape string

const (
	// svgPoint is an instant known to the second or finer.
	svgPoint svgShape = "point"
	// svgBand is an instant known only to its minute, hour or day, spanning that window.
	svgBand svgShape = "band"
	// svgBar is an interval, from when it started to when it ended.
	svgBar svgShape = "bar"
)

// svgLayout is every coordinate of an SVG export's plot, below its header: the axis ticks,
// then the lanes.
type svgLayout struct {
	Height float64
	Ticks  []svgTick
	Lanes  []svgLane
}

// svgTick is a time on the axis, dated when it is the first or starts a new day.
type svgTick struct {
	X          float64
	Time, Date string
}

// svgLane is the band of rows holding one source's events.
type svgLane struct {
	Label, Title, Color string
	Y, Height           float64
	Marks               []svgMark
}

// svgMark is one event in its row: its shape from X to X+Width at Y, and its label, which
// Anchor says starts or ends at LabelX.
type svgMark struct {
	X, Width, Y, LabelX float64
	Shape               svgShape
	// Fuzzy marks a time that is approximate or known only to its minute, hour or day.
	Fuzzy        bool
	Label, Title string
	Anchor       string
}

// layoutSVG lays out events, in occurred at and seq order, on an axis over window with
// ticks on zone's wall clock: a lane per source in order of first appearance, then the
// events with none, each event in its own row.
func layoutSVG(events []Event, window svgWindow, zone *time.Location) svgLayout {
	if len(events) == 0 {
		return svgLayout{}
	}

	axis := svgAxis{window: window}
	layout := svgLayout{Height: svgAxisHeight, Ticks: axis.ticks(zone)}
	palette := sourcePalette()
	for i, g := range groupEvents(events, sourceOf) {
		lane := svgLane{
			Label: "(no source)", Title: "(no source)", Color: svgMuted,
			Y: layout.Height, Height: float64(len(g.events))*svgRowHeight + 2*svgLanePadding,
		}
		if g.key != "" {
			lane.Label, lane.Title, lane.Color = truncate(g.key, svgLaneLabelRunes), g.key, palette[i%len(palette)].Light
		}
		for row, e := range g.events {
			lane.Marks = append(lane.Marks, axis.mark(e, zone, lane.Y+svgLanePadding+(float64(row)+0.5)*svgRowHeight))
		}
		layout.Lanes = append(layout.Lanes, lane)
		layout.Height += lane.Height
	}

	return layout
}

// exportWindow returns the time an export covers: from its query's since or its first event,
// whichever is earlier, to its query's until or the furthest an event's interval or
// precision reaches, whichever is later. A single moment is given a minute.
func exportWindow(q Query, events []Event) svgWindow {
	if len(events) == 0 {
		return svgWindow{}
	}

	var starts, reaches []time.Time
	if q.Since != nil {
		starts = append(starts, time.Time(*q.Since))
	}
	if q.Until != nil {
		reaches = append(reaches, time.Time(*q.Until))
	}
	for _, e := range events {
		start := time.Time(e.OccurredAt)
		starts = append(starts, start)
		reaches = append(reaches, start.Add(precisionWindow(e.Precision)))
		if e.EndedAt != nil {
			reaches = append(reaches, time.Time(*e.EndedAt))
		}
	}

	window := svgWindow{From: slices.MinFunc(starts, time.Time.Compare), To: slices.MaxFunc(reaches, time.Time.Compare)}
	if !window.To.After(window.From) {
		window.To = window.From.Add(time.Minute)
	}

	return window
}

// precisionWindow returns how long a time known to precision may stretch past its start.
func precisionWindow(precision timeline.Precision) time.Duration {
	switch precision {
	case timeline.PrecisionDay:
		return 24 * time.Hour
	case timeline.PrecisionHour:
		return time.Hour
	case timeline.PrecisionMinute:
		return time.Minute
	case timeline.PrecisionSecond, timeline.PrecisionMillisecond, timeline.PrecisionMicrosecond, timeline.PrecisionNanosecond:
	}

	return 0
}

// svgAxis maps times in its window onto the plot.
type svgAxis struct {
	window svgWindow
}

func (axis svgAxis) x(t time.Time) float64 {
	return svgGutter + svgPlotWidth*float64(t.Sub(axis.window.From))/float64(axis.window.To.Sub(axis.window.From))
}

// mark returns e's mark in the row at y, labelled with its time in zone.
func (axis svgAxis) mark(e Event, zone *time.Location, y float64) svgMark {
	start := time.Time(e.OccurredAt)
	end := start.Add(precisionWindow(e.Precision))
	shape := svgBand
	switch {
	case e.EndedAt != nil:
		shape, end = svgBar, time.Time(*e.EndedAt)
	case end.Equal(start):
		shape = svgPoint
	}

	text := svgWhen(e, zone) + " " + strings.TrimSpace(strings.Join(lines(e.Text), " "))
	m := svgMark{
		X: axis.x(start), Y: y, Shape: shape, Fuzzy: e.Approximate || precisionWindow(e.Precision) > 0,
		Label: truncate(text, svgLabelRunes), Title: text,
	}
	m.Width = axis.x(end) - m.X
	m.LabelX, m.Anchor = labelPlace(m.X, m.X+m.Width)

	return m
}

// labelPlace returns where a mark from x from to x to puts its label: after a mark in the
// plot's left half, before one in its right half, and inside one that crosses the middle.
func labelPlace(from, to float64) (float64, string) {
	middle := svgGutter + svgPlotWidth/2
	switch {
	case to <= middle:
		return to + svgLabelGap, "start"
	case from >= middle:
		return from - svgLabelGap, "end"
	}

	return from + svgLabelGap, "start"
}

// svgWhen returns e's time in zone at its precision, a day-precision event's as the date in
// its own offset, then its interval's end, dated when that falls on another day, and ~ first
// when it is approximate.
func svgWhen(e Event, zone *time.Location) string {
	start := time.Time(e.OccurredAt).In(zone)
	when := clock(start, e.Precision, false)
	if e.Precision == timeline.PrecisionDay {
		when = time.Time(e.OccurredAt).In(time.FixedZone("", deref(e.OccurredOffset))).Format(time.DateOnly)
	}
	if e.EndedAt != nil {
		end := time.Time(*e.EndedAt).In(zone)
		endText := clock(end, e.Precision, false)
		if e.Precision == timeline.PrecisionDay || end.Format(time.DateOnly) != start.Format(time.DateOnly) {
			endText = strings.TrimSpace(end.Format(time.DateOnly) + " " + endText)
		}
		when += "–" + endText
	}
	if e.Approximate {
		when = "~" + when
	}

	return when
}

// ticks returns the axis's ticks at round times on zone's wall clock.
func (axis svgAxis) ticks(zone *time.Location) []svgTick {
	step := tickStep(axis.window.To.Sub(axis.window.From))
	layout := "15:04"
	switch {
	case step < time.Minute:
		layout = time.TimeOnly
	case step >= 24*time.Hour:
		layout = ""
	}

	var ticks []svgTick
	previousDate := ""
	for _, t := range tickTimes(axis.window, zone, step) {
		tick := svgTick{X: axis.x(t), Time: t.Format(layout)}
		if date := t.Format(time.DateOnly); date != previousDate {
			tick.Date, previousDate = date, date
		}
		ticks = append(ticks, tick)
	}

	return ticks
}

// tickStep returns the roundest step that puts fewer than svgMaxTicks steps across span.
func tickStep(span time.Duration) time.Duration {
	for _, step := range []time.Duration{
		time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
		time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
		time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
	} {
		if span < svgMaxTicks*step {
			return step
		}
	}

	day := 24 * time.Hour
	days := int64(span/(svgMaxTicks*day)) + 1
	return time.Duration(days) * day
}

// tickTimes returns the times in window a whole number of steps after a midnight in zone.
// ponytail: sub-day steps count absolute time from the first midnight, so on a day the zone
// changes its offset, ticks after the change sit off the wall-clock hour; re-anchor each local
// day if that matters.
func tickTimes(window svgWindow, zone *time.Location, step time.Duration) []time.Time {
	from := window.From.In(zone)
	t := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, zone)
	next := func(t time.Time) time.Time { return t.Add(step) }
	if days := int(step / (24 * time.Hour)); step%(24*time.Hour) == 0 {
		next = func(t time.Time) time.Time { return t.AddDate(0, 0, days) }
		if t.Before(from) {
			t = next(t)
		}
	} else {
		steps := int64((from.Sub(t) + step - 1) / step)
		t = t.Add(time.Duration(steps) * step)
	}

	var times []time.Time
	for ; !t.After(window.To); t = next(t) {
		times = append(times, t)
	}

	return times
}

// truncate returns s cut to at most runes runes, ending in … when cut.
func truncate(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}

	return string([]rune(s)[:runes-1]) + "…"
}
