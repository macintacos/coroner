package export

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"slices"
	"time"
)

// Record is one JSONL record: a Header, Timeline, Source, Event or Annotation.
type Record interface {
	// line returns the record as its JSONL object, with "type" as the first key.
	line() any
	// validate reports a malformed ID, name or tag in the record.
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

// Records returns env's records in export order: the header; the timelines; the annotations
// on no event, by created at then id; the sources, by name; then the events, by occurred at
// then seq, each followed by its annotations, by created at then id. env's own list order does
// not matter. It fails when an annotation's event is not in env.Events.
func Records(env Envelope) ([]Record, error) {
	annotations := slices.SortedFunc(slices.Values(env.Annotations), compareAnnotations)
	byEvent := map[string][]Annotation{}
	records := []Record{env.header()}
	for _, t := range env.Timelines {
		records = append(records, t)
	}
	for _, a := range annotations {
		if a.EventID == nil {
			records = append(records, a)
		} else {
			byEvent[*a.EventID] = append(byEvent[*a.EventID], a)
		}
	}
	for _, s := range slices.SortedFunc(slices.Values(env.Sources), func(a, b Source) int { return cmp.Compare(a.Name, b.Name) }) {
		records = append(records, s)
	}
	for _, e := range slices.SortedFunc(slices.Values(env.Events), compareEvents) {
		records = append(records, e)
		for _, a := range byEvent[e.ID] {
			records = append(records, a)
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

func compareAnnotations(a, b Annotation) int {
	return cmp.Or(time.Time(a.CreatedAt).Compare(time.Time(b.CreatedAt)), cmp.Compare(a.ID, b.ID))
}

func compareEvents(a, b Event) int {
	return cmp.Or(time.Time(a.OccurredAt).Compare(time.Time(b.OccurredAt)), cmp.Compare(deref(a.Seq), deref(b.Seq)))
}

// deref returns what p points to, or the zero value when p is nil.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}
