package export

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"slices"
	"time"
)

// Record is one JSONL record: a Header, Timeline, Source, Event, Annotation or Attachment.
type Record interface {
	// line returns the record as its JSONL object, with "type" as the first key.
	line() any
	// validate reports why the decoded record is not one coroner can read.
	validate() error
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

// Records returns env's records in export order: the header; the timelines, by name; the
// annotations on no event, by created at then id; the sources, by name; then the events, by
// occurred at, timeline id, seq, then id, each followed by its annotations, by created at then id. Every
// event and annotation is followed by its attachments, by created at then id, before anything
// else. env's own list order does not matter. It fails when an annotation's event, or an
// attachment's event or annotation, is not in env.
func Records(env Envelope) ([]Record, error) {
	byOwner, err := groupAttachments(env)
	if err != nil {
		return nil, err
	}

	annotations := slices.SortedFunc(slices.Values(env.Annotations), compareAnnotations)
	byEvent := map[string][]Annotation{}
	records := []Record{env.header()}
	appendWithAttachments := func(r Record, ownerID string) {
		records = append(records, r)
		for _, f := range byOwner[ownerID] {
			records = append(records, f)
		}
	}
	for _, t := range slices.SortedFunc(slices.Values(env.Timelines), compareTimelines) {
		records = append(records, t)
	}
	for _, a := range annotations {
		if a.EventID == nil {
			appendWithAttachments(a, a.ID)
		} else {
			byEvent[*a.EventID] = append(byEvent[*a.EventID], a)
		}
	}
	for _, s := range slices.SortedFunc(slices.Values(env.Sources), compareSources) {
		records = append(records, s)
	}
	for _, e := range slices.SortedFunc(slices.Values(env.Events), compareEvents) {
		appendWithAttachments(e, e.ID)
		for _, a := range byEvent[e.ID] {
			appendWithAttachments(a, a.ID)
		}
		delete(byEvent, e.ID)
	}

	// Every event's annotations were written and deleted above, so any left are orphans.
	for _, a := range annotations {
		if a.EventID != nil && byEvent[*a.EventID] != nil {
			return nil, fmt.Errorf("annotation %s: its event %s is not in the export", a.ID, *a.EventID)
		}
	}

	return records, nil
}

// WriteRecords writes each record as one JSONL line, and writes nothing to w when any record
// fails to encode.
func WriteRecords(w io.Writer, records []Record) error {
	var b bytes.Buffer
	for _, r := range records {
		err := Write(&b, r.line())
		if err != nil {
			return err
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

// groupAttachments returns env's attachments by created at then id, keyed by the id of the
// event or annotation that keeps each, and fails naming the first whose owner is not in env.
func groupAttachments(env Envelope) (map[string][]Attachment, error) {
	events, annotations := map[string]bool{}, map[string]bool{}
	for _, e := range env.Events {
		events[e.ID] = true
	}
	for _, a := range env.Annotations {
		annotations[a.ID] = true
	}

	byOwner := map[string][]Attachment{}
	for _, f := range slices.SortedFunc(slices.Values(env.Attachments), compareAttachments) {
		switch {
		case f.EventID != nil && !events[*f.EventID]:
			return nil, fmt.Errorf("attachment %s: its event %s is not in the export", f.ID, *f.EventID)
		case f.AnnotationID != nil && !annotations[*f.AnnotationID]:
			return nil, fmt.Errorf("attachment %s: its annotation %s is not in the export", f.ID, *f.AnnotationID)
		}
		ownerID := deref(cmp.Or(f.EventID, f.AnnotationID))
		byOwner[ownerID] = append(byOwner[ownerID], f)
	}

	return byOwner, nil
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
