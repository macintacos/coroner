package export

import (
	"bytes"
	"cmp"
	_ "embed"
	"fmt"
	"io"
	"slices"
	"text/template"
	"time"
)

//go:embed html/index.html.tmpl
var pageShell string

//go:embed html/viewer.css
var viewerCSS string

//go:embed html/viewer.js
var viewerJS string

//go:embed html/vendor/vis-timeline-graph2d.min.js
var visTimeline string

// htmlPage is what the page shell interpolates.
type htmlPage struct {
	CSS, Export, Zone, Library, Viewer string
}

// pageZone is what the viewer needs to show times in the export's zone: its name, and its
// offsets from UTC over the time the page covers.
type pageZone struct {
	Name  string     `json:"name"`
	Spans []zoneSpan `json:"spans"`
}

// zoneSpan is a stretch of time over which a zone keeps one offset and abbreviation. It lasts
// until the next span's start; the first span also stands for all time before it, and the
// last for all time after.
type zoneSpan struct {
	// Start is in milliseconds since the Unix epoch, which JavaScript reads as a Date.
	Start int64 `json:"start"`
	// Offset is in seconds east of UTC.
	Offset       int    `json:"offset"`
	Abbreviation string `json:"abbreviation"`
}

// WriteHTML writes env's timeline to w as one self-contained HTML page: an interactive
// timeline of its events with one row per source, showing times in zone (nil is UTC). The
// page embeds env as the JSON Write encodes, its events by occurred at and seq, with the
// viewer and its library inline, so it loads nothing over the network. It fails, writing
// nothing, unless env holds one timeline and the event of every annotation.
func WriteHTML(w io.Writer, env Envelope, zone *time.Location) error {
	if len(env.Timelines) != 1 {
		return fmt.Errorf("want one timeline, got %d", len(env.Timelines))
	}
	env.Events = slices.SortedFunc(slices.Values(env.Events), compareEvents)
	_, _, err := groupAnnotations(env.Annotations, env.Events)
	if err != nil {
		return err
	}
	zone = cmp.Or(zone, time.UTC)

	var exported, zoneData bytes.Buffer
	err = Write(&exported, env)
	if err != nil {
		return err
	}
	err = Write(&zoneData, pageZone{Name: pageZoneName(zone), Spans: zoneSpans(zone, pageInstants(env))})
	if err != nil {
		return err
	}

	// text/template, not html/template: html/template would need template.JS and template.CSS
	// conversions, which gosec flags, and lint is never suppressed. Nothing is escaped, so each
	// value is JSON from Write, which escapes <, > and &, or an embedded file with no </script,
	// </style or <!--; specs pin both.
	shell, err := template.New("page").Parse(pageShell)
	if err != nil {
		return err
	}
	var page bytes.Buffer
	err = shell.Execute(&page, htmlPage{
		CSS: viewerCSS, Export: exported.String(), Zone: zoneData.String(), Library: visTimeline, Viewer: viewerJS,
	})
	if err != nil {
		return err
	}

	_, err = w.Write(page.Bytes())
	return err
}

// pageZoneName returns how the page names zone. The page is shared, so time.Local, named
// Local, is named as the exporter's rather than read as the zone of whoever opens it.
func pageZoneName(zone *time.Location) string {
	if zone.String() == "Local" {
		return "the exporter's local zone"
	}

	return zone.String()
}

// pageInstants returns every instant env records that a reader of the page may look at: when
// it was exported, and when each event occurred and ended.
func pageInstants(env Envelope) []time.Time {
	instants := []time.Time{time.Time(env.ExportedAt)}
	for _, e := range env.Events {
		instants = append(instants, eventInstants(e)...)
	}

	return instants
}

// zoneSpans returns zone's spans from a year before the earliest of instants to a year after
// the latest, so panning a little past the events still shows times in zone.
func zoneSpans(zone *time.Location, instants []time.Time) []zoneSpan {
	from := slices.MinFunc(instants, time.Time.Compare).AddDate(-1, 0, 0)
	to := slices.MaxFunc(instants, time.Time.Compare).AddDate(1, 0, 0)

	var spans []zoneSpan
	for t := from.In(zone); ; {
		name, offset := t.Zone()
		spans = append(spans, zoneSpan{Start: t.UnixMilli(), Offset: offset, Abbreviation: name})
		_, end := t.ZoneBounds()
		if end.IsZero() || end.After(to) {
			return spans
		}
		t = end
	}
}
