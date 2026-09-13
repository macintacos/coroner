package export

import (
	"bytes"
	"cmp"
	"embed"
	"fmt"
	"io"
	"slices"
	"text/template"
	"time"
)

//go:embed html/index.html.tmpl html/viewer.css html/viewer.js html/vendor/vis-timeline-graph2d.min.js
var htmlFiles embed.FS

// pageZone is what the viewer needs to show times in the export's zone: its name, and its
// offsets from UTC over the time the page covers.
type pageZone struct {
	Name  string     `json:"name"`
	Spans []zoneSpan `json:"spans"`
}

// zoneSpan is a stretch of time over which a zone keeps one offset and abbreviation. It lasts
// until the next span's start, and the first span also stands for all time before it.
type zoneSpan struct {
	// Start is in milliseconds since the Unix epoch, which JavaScript reads as a Date.
	Start        int64  `json:"start"`
	Offset       int    `json:"offset"`
	Abbreviation string `json:"abbreviation"`
}

// WriteHTML writes env's timeline to w as one self-contained HTML page: an interactive
// timeline of its events with one row per source, showing times in zone (nil is UTC). The
// page embeds env as the JSON Write encodes, with the viewer and its library inline, so it
// loads nothing over the network. It fails, writing nothing, unless env holds one timeline.
func WriteHTML(w io.Writer, env Envelope, zone *time.Location) error {
	if len(env.Timelines) != 1 {
		return fmt.Errorf("want one timeline, got %d", len(env.Timelines))
	}
	zone = cmp.Or(zone, time.UTC)

	var exported, zoneData bytes.Buffer
	err := Write(&exported, env)
	if err != nil {
		return err
	}
	err = Write(&zoneData, pageZone{Name: zone.String(), Spans: zoneSpans(zone, pageInstants(env))})
	if err != nil {
		return err
	}

	// text/template inlines each value as is. That is safe only because none holds text a
	// person typed except as JSON, which Write escapes <, > and & in, and the files hold no
	// </script: the viewer puts every string from the export on the page as text.
	shell := template.Must(template.New("index.html.tmpl").Parse(htmlFile("index.html.tmpl")))
	var b bytes.Buffer
	err = shell.Execute(&b, map[string]string{
		"CSS":     htmlFile("viewer.css"),
		"Export":  exported.String(),
		"Zone":    zoneData.String(),
		"Library": htmlFile("vendor/vis-timeline-graph2d.min.js"),
		"Viewer":  htmlFile("viewer.js"),
	})
	if err != nil {
		return err
	}

	_, err = w.Write(b.Bytes())
	return err
}

func htmlFile(name string) string {
	// Embedded files cannot fail to read.
	b, _ := htmlFiles.ReadFile("html/" + name)
	return string(b)
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
