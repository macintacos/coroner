package export

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"slices"
	"time"
)

// Record is one JSONL record: a Header, Timeline, Source, Event, Annotation, Attachment or
// Link.
type Record interface {
	// line returns the record as its JSONL object, with "type" as the first key.
	line() any
	// validate reports why the decoded record is not one coroner can read.
	validate() error
	// name returns "header", or the record's type and id, for an error to name it by.
	name() string
}

// The JSONL objects embed the record, which json/v2 inlines after "type".
func (h Header) line() any {
	return struct {
		Type string `json:"type"`
		Header
	}{"header", h}
}

func (t Timeline) line() any {
	return struct {
		Type string `json:"type"`
		Timeline
	}{"timeline", t}
}

func (s Source) line() any {
	return struct {
		Type string `json:"type"`
		Source
	}{"source", s}
}

func (e Event) line() any {
	return struct {
		Type string `json:"type"`
		Event
	}{"event", e}
}

func (a Annotation) line() any {
	return struct {
		Type string `json:"type"`
		Annotation
	}{"annotation", a}
}

func (a Attachment) line() any {
	return struct {
		Type string `json:"type"`
		Attachment
	}{"attachment", a}
}

func (l Link) line() any {
	return struct {
		Type string `json:"type"`
		Link
	}{"link", l}
}

func (Header) name() string       { return "header" }
func (t Timeline) name() string   { return "timeline " + t.ID }
func (s Source) name() string     { return "source " + s.ID }
func (e Event) name() string      { return "event " + e.ID }
func (a Annotation) name() string { return "annotation " + a.ID }
func (a Attachment) name() string { return "attachment " + a.ID }
func (l Link) name() string       { return "link " + l.ID }

// Records returns env's records in export order: the header; the timelines, by name; the
// annotations on no event, by created at then id; the sources, by name; the events, by
// occurred at, timeline id, seq, then id, each followed by its annotations, by created at
// then id; then the links, by created at then id. Every event and annotation is followed by
// its attachments, by created at then id, before anything else. env's own list order does not
// matter. It fails unless env is consistent (see prepare).
func Records(env Envelope) ([]Record, error) {
	v, err := prepare(env)
	if err != nil {
		return nil, err
	}

	records := []Record{env.header()}
	appendWithAttachments := func(r Record, ownerID string) {
		records = append(records, r)
		for _, f := range v.Attachments[ownerID] {
			records = append(records, f)
		}
	}
	for _, t := range slices.SortedFunc(slices.Values(v.Timelines), compareTimelines) {
		records = append(records, t)
	}
	for _, a := range v.OnTimeline {
		appendWithAttachments(a, a.ID)
	}
	for _, s := range slices.SortedFunc(slices.Values(env.Sources), compareSources) {
		records = append(records, s)
	}
	for _, e := range v.Events {
		appendWithAttachments(e, e.ID)
		for _, a := range v.ByEvent[e.ID] {
			appendWithAttachments(a, a.ID)
		}
	}
	for _, l := range v.Links {
		records = append(records, l)
	}

	return records, nil
}

func compareLinks(a, b Link) int {
	return cmp.Or(time.Time(a.CreatedAt).Compare(time.Time(b.CreatedAt)), cmp.Compare(a.ID, b.ID))
}

// WriteRecords writes each record as one JSONL line, and writes nothing to w when any record
// fails to encode.
func WriteRecords(w io.Writer, records []Record) error {
	var b bytes.Buffer
	for _, r := range records {
		err := Write(&b, r.line())
		if err != nil {
			return fmt.Errorf("%s: %w", r.name(), err)
		}
	}

	_, err := w.Write(b.Bytes())
	return err
}

func compareTimelines(a, b Timeline) int {
	return cmp.Compare(a.Name, b.Name)
}

func compareSources(a, b Source) int {
	return cmp.Compare(a.Name, b.Name)
}

func compareAnnotations(a, b Annotation) int {
	return cmp.Or(time.Time(a.CreatedAt).Compare(time.Time(b.CreatedAt)), cmp.Compare(a.ID, b.ID))
}

func compareAttachments(a, b Attachment) int {
	return cmp.Or(time.Time(a.CreatedAt).Compare(time.Time(b.CreatedAt)), cmp.Compare(a.ID, b.ID))
}

func compareEvents(a, b Event) int {
	return cmp.Or(
		time.Time(a.OccurredAt).Compare(time.Time(b.OccurredAt)), cmp.Compare(a.TimelineID, b.TimelineID),
		cmp.Compare(deref(a.Seq), deref(b.Seq)), cmp.Compare(a.ID, b.ID),
	)
}

// deref returns what p points to, or the zero value when p is nil.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}
