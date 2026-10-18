package export_test

import (
	"bytes"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 9, 11, hour, minute, 0, 0, time.UTC)
}

func instant(hour, minute int) export.Instant {
	return export.Instant(at(hour, minute))
}

// label names a record by its type and ID, or its type alone for a header.
func label(r export.Record) string {
	switch r := r.(type) {
	case export.Header:
		return "header"
	case export.Timeline:
		return "timeline " + r.ID
	case export.Source:
		return "source " + r.ID
	case export.Event:
		return "event " + r.ID
	case export.Annotation:
		return "annotation " + r.ID
	}

	return "unknown"
}

func labels(records []export.Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, label(r))
	}

	return out
}

var _ = Describe("NewEvent", func() {
	It("carries every field of a stored event, with its times as instants", func() {
		ended := at(3, 20)
		e := timeline.Event{
			ID: "e1", TimelineID: "t1", Seq: 7, OccurredAt: at(3, 14), OccurredOffset: new(7200),
			Precision: timeline.PrecisionMinute, Approximate: true, OccurredRaw: new("05:14"), EndedAt: &ended,
			Source: new("mongod"), Tags: []string{"election"}, Text: "Primary stepped down", URL: new("https://x.test/1"),
			Author: new("julian"), CreatedAt: at(4, 0), UpdatedAt: at(4, 5),
		}

		Expect(export.NewEvent(e)).To(Equal(export.Event{
			ID: "e1", TimelineID: "t1", Seq: new(int64(7)), OccurredAt: instant(3, 14), OccurredOffset: new(7200),
			Precision: timeline.PrecisionMinute, Approximate: true, OccurredRaw: new("05:14"), EndedAt: new(instant(3, 20)),
			Source: new("mongod"), Tags: []string{"election"}, Text: "Primary stepped down", URL: new("https://x.test/1"),
			Author: new("julian"), CreatedAt: instant(4, 0), UpdatedAt: instant(4, 5),
		}))
	})

	It("has a null seq for an event not yet added", func() {
		Expect(export.NewEvent(timeline.Event{}).Seq).To(BeNil())
	})
})

var _ = Describe("Records", func() {
	event := func(id string, seq int64, occurred time.Time) export.Event {
		return export.Event{ID: id, Seq: &seq, OccurredAt: export.Instant(occurred)}
	}
	annotation := func(id string, eventID *string, created time.Time) export.Annotation {
		return export.Annotation{ID: id, EventID: eventID, CreatedAt: export.Instant(created)}
	}

	It("orders records header, timeline, timeline annotations, sources, then each event followed by its annotations", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		env.Timelines = []export.Timeline{{ID: "t1"}}
		env.Sources = []export.Source{{ID: "pagerduty", Name: "pagerduty"}, {ID: "mongod", Name: "mongod"}}
		// e1 and e2 happened at the same moment, so seq orders them.
		env.Events = []export.Event{event("e3", 3, at(3, 15)), event("e2", 2, at(3, 14)), event("e1", 1, at(3, 14))}
		env.Annotations = []export.Annotation{
			annotation("a-e1-late", new("e1"), at(11, 0)),
			annotation("a-t-2", nil, at(10, 0)),
			annotation("a-e2", new("e2"), at(9, 0)),
			// Created earlier than a-e1-late but with a larger id: created at decides first.
			annotation("z-e1-early", new("e1"), at(10, 30)),
			annotation("a-t-1", nil, at(10, 0)),
		}

		records, err := export.Records(env)

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{
			"header", "timeline t1", "annotation a-t-1", "annotation a-t-2", "source mongod", "source pagerduty",
			"event e1", "annotation z-e1-early", "annotation a-e1-late", "event e2", "annotation a-e2", "event e3",
		}))
		Expect(records[0]).To(Equal(export.Header{
			Format: "coroner.export", FormatVersion: "1.0", CoronerVersion: "0.1.0", ExportedAt: instant(9, 0), Query: env.Query,
		}))
	})

	It("orders timelines by name", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		env.Timelines = []export.Timeline{{ID: "t1", Name: "redis-outage"}, {ID: "t2", Name: "mongo-outage"}}

		records, err := export.Records(env)

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{"header", "timeline t2", "timeline t1"}))
	})

	DescribeTable("orders events tying on occurred at and seq by id, whatever their input order",
		func(ids ...string) {
			env := export.NewEnvelope("0.1.0", at(9, 0))
			for _, id := range ids {
				env.Events = append(env.Events, event(id, 1, at(3, 14)))
			}

			records, err := export.Records(env)

			Expect(err).NotTo(HaveOccurred())
			Expect(labels(records)).To(Equal([]string{"header", "event e1", "event e2"}))
		},
		Entry("in id order", "e1", "e2"),
		Entry("in reverse id order", "e2", "e1"),
	)

	It("fails naming an annotation whose event is not in the export", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		env.Annotations = []export.Annotation{annotation("a1", new("gone"), at(10, 0))}

		_, err := export.Records(env)

		Expect(err).To(MatchError(And(ContainSubstring("a1"), ContainSubstring("gone"))))
	})
})

var _ = Describe("WriteRecords", func() {
	It("writes each record as one line with its type first", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		records := []export.Record{
			export.Header{Format: env.Format, FormatVersion: env.FormatVersion, CoronerVersion: "0.1.0", ExportedAt: env.ExportedAt, Query: env.Query},
			export.Source{ID: "s1", Name: "mongod", CreatedAt: instant(4, 0)},
		}
		var b bytes.Buffer

		Expect(export.WriteRecords(&b, records)).To(Succeed())
		Expect(b.String()).To(Equal(
			`{"type":"header","format":"coroner.export","format_version":"1.0","coroner_version":"0.1.0","exported_at":"2026-09-11T09:00:00.000000000Z",` +
				`"query":{"timelines":[],"since":null,"until":null,"sources":[],"tags":[],"contains":null,"limit":null,"order":"asc"}}` + "\n" +
				`{"type":"source","id":"s1","name":"mongod","created_at":"2026-09-11T04:00:00.000000000Z"}` + "\n"))
	})

	It("writes nothing when a record cannot be encoded", func() {
		var b bytes.Buffer

		err := export.WriteRecords(&b, []export.Record{export.Source{Name: "mongod"}, export.Timeline{Title: "\xff"}})

		Expect(err).To(MatchError(ContainSubstring("invalid UTF-8")))
		Expect(b.String()).To(BeEmpty())
	})
})
