package export_test

import (
	"bytes"
	"strings"
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
	case export.Attachment:
		return "attachment " + r.ID
	case export.Link:
		return "link " + r.ID
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

var _ = Describe("NewAttachment", func() {
	It("carries every field of a stored attachment, with its owner's id and its time as an instant", func() {
		a := timeline.Attachment{
			ID: "f1", Owner: timeline.AttachmentOwner{AnnotationID: new("a1")}, Filename: "p99.png", MIME: "image/png",
			SHA256: strings.Repeat("ab", 32), Size: 2048, CreatedAt: at(4, 0),
		}

		Expect(export.NewAttachment(a)).To(Equal(export.Attachment{
			ID: "f1", AnnotationID: new("a1"), Filename: "p99.png", MIME: "image/png", SHA256: strings.Repeat("ab", 32), Size: 2048,
			CreatedAt: instant(4, 0),
		}))
	})
})

var _ = Describe("Records", func() {
	event := func(id string, seq int64, occurred time.Time) export.Event {
		return export.Event{ID: id, Seq: &seq, OccurredAt: export.Instant(occurred)}
	}
	annotation := func(id string, eventID *string, created time.Time) export.Annotation {
		return export.Annotation{ID: id, EventID: eventID, CreatedAt: export.Instant(created)}
	}
	attachment := func(id string, owner timeline.AttachmentOwner, created time.Time) export.Attachment {
		return export.Attachment{ID: id, EventID: owner.EventID, AnnotationID: owner.AnnotationID, CreatedAt: export.Instant(created)}
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
			Format: "coroner.export", FormatVersion: "1.2", CoronerVersion: "0.1.0", ExportedAt: instant(9, 0), Query: env.Query,
		}))
	})

	It("follows each event and annotation with its attachments, by created at then id", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		env.Events = []export.Event{event("e2", 2, at(3, 15)), event("e1", 1, at(3, 14))}
		env.Annotations = []export.Annotation{annotation("a-t", nil, at(10, 0)), annotation("a-e1", new("e1"), at(10, 0))}
		env.Attachments = []export.Attachment{
			attachment("f-e1-late", timeline.AttachmentOwner{EventID: new("e1")}, at(12, 0)),
			attachment("f-a-e1", timeline.AttachmentOwner{AnnotationID: new("a-e1")}, at(11, 0)),
			attachment("f-e2", timeline.AttachmentOwner{EventID: new("e2")}, at(11, 0)),
			// Created earlier than f-e1-late but with a larger id: created at decides first.
			attachment("z-e1-early", timeline.AttachmentOwner{EventID: new("e1")}, at(11, 0)),
			attachment("f-a-t", timeline.AttachmentOwner{AnnotationID: new("a-t")}, at(11, 0)),
		}

		records, err := export.Records(env)

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{
			"header", "annotation a-t", "attachment f-a-t", "event e1", "attachment z-e1-early", "attachment f-e1-late",
			"annotation a-e1", "attachment f-a-e1", "event e2", "attachment f-e2",
		}))
	})

	It("orders timelines by name", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		env.Timelines = []export.Timeline{{ID: "t1", Name: "redis-outage"}, {ID: "t2", Name: "mongo-outage"}}

		records, err := export.Records(env)

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{"header", "timeline t2", "timeline t1"}))
	})

	It("orders events of several timelines at one instant by timeline id, then seq", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		b1, a2, a1 := event("b1", 1, at(3, 14)), event("a2", 2, at(3, 14)), event("a1", 1, at(3, 14))
		b1.TimelineID, a2.TimelineID, a1.TimelineID = "tb", "ta", "ta"
		env.Events = []export.Event{b1, a2, a1}

		records, err := export.Records(env)

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{"header", "event a1", "event a2", "event b1"}))
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

	DescribeTable("fails naming an attachment whose owner is not in the export",
		func(owner timeline.AttachmentOwner) {
			env := export.NewEnvelope("0.1.0", at(9, 0))
			env.Events = []export.Event{event("e1", 1, at(3, 14))}
			env.Annotations = []export.Annotation{annotation("a1", new("e1"), at(10, 0))}
			env.Attachments = []export.Attachment{attachment("f1", owner, at(11, 0))}

			_, err := export.Records(env)

			Expect(err).To(MatchError(And(ContainSubstring("f1"), ContainSubstring("gone"))))
		},
		Entry("an event", timeline.AttachmentOwner{EventID: new("gone")}),
		Entry("an annotation", timeline.AttachmentOwner{AnnotationID: new("gone")}),
	)

	link := func(id, from, to string, created time.Time) export.Link {
		return export.Link{ID: id, FromEventID: from, ToEventID: to, CreatedAt: export.Instant(created)}
	}

	It("ends with the links, by created at then id, after every event they join", func() {
		env := export.NewEnvelope("0.1.0", at(9, 0))
		b1, a1 := event("b1", 1, at(3, 15)), event("a1", 1, at(3, 14))
		b1.TimelineID, a1.TimelineID = "tb", "ta"
		env.Events = []export.Event{b1, a1}
		env.Annotations = []export.Annotation{annotation("n-b1", new("b1"), at(10, 0))}
		env.Links = []export.Link{
			link("l-late", "b1", "a1", at(12, 0)),
			link("l-cross", "a1", "b1", at(11, 0)),
			// Created earlier than l-late but with a larger id: created at decides first.
			link("z-early", "b1", "a1", at(11, 0)),
		}

		records, err := export.Records(env)

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{
			"header", "event a1", "event b1", "annotation n-b1", "link l-cross", "link z-early", "link l-late",
		}))
	})

	DescribeTable("fails naming a link whose event is not in the export",
		func(from, to string) {
			env := export.NewEnvelope("0.1.0", at(9, 0))
			env.Events = []export.Event{event("e1", 1, at(3, 14))}
			env.Links = []export.Link{link("l1", from, to, at(11, 0))}

			_, err := export.Records(env)

			Expect(err).To(MatchError(And(ContainSubstring("l1"), ContainSubstring("gone"))))
		},
		Entry("the event it leaves", "gone", "e1"),
		Entry("the event it reaches", "e1", "gone"),
	)
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
			`{"type":"header","format":"coroner.export","format_version":"1.2","coroner_version":"0.1.0","exported_at":"2026-09-11T09:00:00.000000000Z",` +
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
