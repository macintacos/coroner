package export

import (
	"bytes"
	"cmp"
	_ "embed"
	"encoding/xml"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/macintacos/coroner/internal/timeline"
)

//go:embed svg/timeline.svg.tmpl
var svgShell string

const (
	svgHeaderRunes = 110
	svgWidth       = 1224.0
	svgGutter      = 200.0
	svgMargin      = 24.0
	// svgRightMargin is wide enough for the half of the last tick's date that overhangs it.
	svgRightMargin   = 2 * svgMargin
	svgPlotWidth     = svgWidth - svgGutter - svgRightMargin
	svgTitleBaseline = 34.0
	svgTitleLeading  = 24.0
	svgHeaderLeading = 18.0
	svgPlotGap       = 4.0
	svgNoteGap       = 10.0
	svgAxisHeight    = 40.0
	svgRowHeight     = 22.0
	svgLanePadding   = 6.0
	svgLabelGap      = 8.0
	svgMinBarWidth   = 2.0
	// svgLabelRunes and svgLaneLabelRunes cut labels short, since an SVG cannot measure text;
	// the full text stays in a tooltip. svgLabelRunes is short enough to fit half the plot,
	// which labelPlace relies on.
	svgLabelRunes     = 60
	svgLaneLabelRunes = 24
	svgMaxTicks       = 8
	// svgMuted colours the lane of events with no source.
	svgMuted = "#545c63"
)

// svgPicture is what the SVG template prints: a title at TitleY and header lines, then the
// plot, translated down to PlotY, or a note at NoteY when there are no events. The title,
// header lines, note and lane labels start at Margin.
type svgPicture struct {
	Width, Height, Margin, TitleY, PlotY, NoteY float64
	// AxisY is the axis line's y in the plot, below the tick labels.
	AxisY float64
	// Label is cut to fit the header; Title names the picture.
	Label, Title string
	Header       []svgText
	Plot         svgLayout
}

// svgText is a line of header text with its baseline.
type svgText struct {
	Y    float64
	Text string
}

// WriteSVG writes env's timeline to w as a static SVG picture: a header naming the timeline,
// its refs, the zone and when it was exported, then a time axis over the export's window with
// ticks on zone's wall clock (nil is UTC), and a lane per source in order of first appearance,
// then (no source), with each event in its own row. It fails, writing nothing, unless env
// holds one timeline.
func WriteSVG(w io.Writer, env Envelope, zone *time.Location) error {
	if len(env.Timelines) != 1 {
		return fmt.Errorf("want one timeline, got %d", len(env.Timelines))
	}

	// text/template escapes nothing, so every string from the export goes through text.
	shell, err := template.New("svg").Funcs(template.FuncMap{"text": xmlText, "num": svgNumber, "add": add}).Parse(svgShell)
	if err != nil {
		return err
	}
	var picture bytes.Buffer
	err = shell.Execute(&picture, newSVGPicture(env, cmp.Or(zone, time.UTC)))
	if err != nil {
		return err
	}

	_, err = w.Write(picture.Bytes())
	return err
}

// newSVGPicture returns what the template prints for env in zone.
func newSVGPicture(env Envelope, zone *time.Location) svgPicture {
	t := env.Timelines[0]
	title := flatten(t.Name)
	if strings.TrimSpace(t.Title) != "" {
		title = flatten(t.Title) + " (" + flatten(t.Name) + ")"
	}

	var header []string
	if len(t.Refs) > 0 {
		header = append(header, flatten(strings.Join(t.Refs, ", ")))
	}
	exported := time.Time(env.ExportedAt).In(zone)
	header = append(header,
		"Times in "+pageZoneName(zone),
		"Exported "+exported.Format("2006-01-02 15:04")+" "+zoneName(exported)+" by coroner "+flatten(env.CoronerVersion),
	)

	picture := svgPicture{
		Width: svgWidth, Margin: svgMargin, TitleY: svgTitleBaseline, AxisY: svgAxisHeight,
		Label: truncate(title, svgHeaderRunes), Title: title,
	}
	y := svgTitleBaseline + svgTitleLeading
	for _, line := range header {
		picture.Header = append(picture.Header, svgText{Y: y, Text: truncate(line, svgHeaderRunes)})
		y += svgHeaderLeading
	}

	events := slices.SortedFunc(slices.Values(env.Events), compareEvents)
	picture.PlotY, picture.NoteY = y+svgPlotGap, y+svgNoteGap
	picture.Plot = layoutSVG(events, exportWindow(env.Query, events), zone)
	picture.Height = picture.PlotY + picture.Plot.Height + svgMargin
	if len(events) == 0 {
		picture.Height = picture.NoteY + svgMargin
	}

	return picture
}

// xmlText escapes s as XML text or attribute content, writing U+FFFD for what XML cannot hold,
// such as control characters and invalid UTF-8.
func xmlText(s string) (string, error) {
	var b strings.Builder
	err := xml.EscapeText(&b, []byte(s))

	return b.String(), err
}

// svgNumber formats a coordinate to two decimals, so output never depends on float printing.
func svgNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func add(a, b float64) float64 {
	return a + b
}

// flatten returns s on one line, trimmed.
func flatten(s string) string {
	return strings.TrimSpace(strings.Join(lines(s), " "))
}

// svgWindow is the stretch of time the axis spans.
type svgWindow struct{ From, To time.Time }

// svgShape is how a mark draws an event. Its values are the template's vocabulary: the
// template compares against them.
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

// svgTick is a time on the axis, dated when it is the first or starts a new day. Time is empty
// when ticks step by days.
type svgTick struct {
	X          float64
	Time, Date string
}

// svgLane is the band of rows from Y to Bottom holding one source's events, labelled on the
// centre line of its first row, LabelY.
type svgLane struct {
	Label, Title, Color string
	Y, Bottom, LabelY   float64
	Marks               []svgMark
}

// svgMark is one event in its row: its shape from X to X+Width at Y, and its label at LabelX.
// Label is cut to svgLabelRunes; Title holds the whole text for the tooltip. Anchor is an SVG
// text-anchor, start or end.
type svgMark struct {
	X, Width, Y, LabelX float64
	Shape               svgShape
	// Inexact marks a time that is approximate, or known only to its minute, hour or day.
	Inexact      bool
	Label, Title string
	Anchor       string
}

// layoutSVG lays out events, in occurred at and seq order, on an axis over window with
// ticks on zone's wall clock: a lane per source in order of first appearance, then the
// events with none, each event in its own row. With events, window must not be empty;
// exportWindow's never is.
func layoutSVG(events []Event, window svgWindow, zone *time.Location) svgLayout {
	if len(events) == 0 {
		return svgLayout{}
	}

	axis := svgAxis{window: window}
	layout := svgLayout{Height: svgAxisHeight, Ticks: axis.ticks(zone)}
	palette := sourcePalette()
	for i, g := range groupEvents(events, sourceOf) {
		lane := svgLane{Label: "(no source)", Title: "(no source)", Color: svgMuted, Y: layout.Height}
		if g.key != "" {
			lane.Label, lane.Title, lane.Color = truncate(g.key, svgLaneLabelRunes), g.key, palette[i%len(palette)].Light
		}
		for row, e := range g.events {
			lane.Marks = append(lane.Marks, axis.mark(e, zone, lane.Y+svgLanePadding+(float64(row)+0.5)*svgRowHeight))
		}
		lane.LabelY = lane.Marks[0].Y
		lane.Bottom = lane.Y + float64(len(g.events))*svgRowHeight + 2*svgLanePadding
		layout.Lanes = append(layout.Lanes, lane)
		layout.Height = lane.Bottom
	}

	return layout
}

// exportWindow returns the time an export covers: from its query's since or its first event,
// whichever is earlier, to its query's until or the furthest an event's interval or
// precision reaches, whichever is later. A window shorter than a minute is widened to one.
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
	if window.To.Sub(window.From) < time.Minute {
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

	text := svgWhen(e, zone) + " " + flatten(e.Text)
	m := svgMark{
		X: axis.x(start), Y: y, Shape: shape, Inexact: e.Approximate || precisionWindow(e.Precision) > 0,
		Label: truncate(text, svgLabelRunes), Title: text,
	}
	m.Width = axis.x(end) - m.X
	if shape == svgBar {
		m.Width = max(m.Width, svgMinBarWidth)
	}
	m.LabelX, m.Anchor = labelPlace(m.X, m.X+m.Width)

	return m
}

// labelPlace returns the x and text-anchor of the label of a mark spanning from to to: after
// a mark in the plot's left half, before one in its right half, and inside one that crosses
// the middle.
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
// its own offset, then its interval's end, dated when it is day precision or falls on another
// day in zone, and ~ first when it is approximate.
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
func tickTimes(window svgWindow, zone *time.Location, step time.Duration) []time.Time {
	day := 24 * time.Hour
	if step%day == 0 {
		return dayTicks(window, zone, int(step/day))
	}

	return clockTicks(window, zone, step)
}

// dayTicks returns every days-th midnight in zone in window, from the first at or after its
// start.
func dayTicks(window svgWindow, zone *time.Location, days int) []time.Time {
	from := window.From.In(zone)
	t := startOfDay(from)
	if t.Before(from) {
		t = t.AddDate(0, 0, 1)
	}

	var times []time.Time
	for ; !t.After(window.To); t = t.AddDate(0, 0, days) {
		times = append(times, t)
	}

	return times
}

// clockTicks returns the times in window a whole number of steps after the midnight in zone
// that starts its first day.
// ponytail: sub-day steps count absolute time from the first midnight, so once the zone
// changes its offset inside the window every later tick sits off the wall-clock hour;
// re-anchor on each local midnight if that matters.
func clockTicks(window svgWindow, zone *time.Location, step time.Duration) []time.Time {
	from := window.From.In(zone)
	midnight := startOfDay(from)
	steps := int64((from.Sub(midnight) + step - 1) / step)

	var times []time.Time
	for t := midnight.Add(time.Duration(steps) * step); !t.After(window.To); t = t.Add(step) {
		times = append(times, t)
	}

	return times
}

// startOfDay returns the midnight that starts t's day in t's location.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// truncate returns s cut to at most runes runes, ending in … when cut.
// ponytail: runes are not widths, so wide glyphs such as CJK and emoji can overrun half the
// plot, and a cut can split a grapheme cluster; measure by East Asian width and cut on
// grapheme boundaries if that matters.
func truncate(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}

	return string([]rune(s)[:runes-1]) + "…"
}
