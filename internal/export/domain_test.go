package export_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

// roundTrip returns record's domain value turned back into a record by its New function.
func roundTrip(record any) any {
	switch r := record.(type) {
	case export.Timeline:
		return export.NewTimeline(r.Domain())
	case export.Source:
		return export.NewSource(r.Domain())
	case export.Event:
		return export.NewEvent(r.Domain())
	case export.Annotation:
		return export.NewAnnotation(r.Domain())
	case export.Attachment:
		return export.NewAttachment(r.Domain())
	}
	Fail("no round trip for this record type")

	return nil
}

var _ = DescribeTable("Domain returns a record's domain value, which New turns back into the same record",
	func(record any) {
		Expect(roundTrip(record)).To(Equal(record))
	},
	Entry("a timeline", export.Timeline{
		ID: timelineID, Name: "mongo-outage", Title: "Mongo outage", Refs: []string{"INC-4821"}, CreatedAt: instant(1, 0),
	}),
	Entry("an untitled timeline with no refs", export.Timeline{
		ID: timelineID, Name: "mongo-outage", Refs: []string{}, CreatedAt: instant(1, 0),
	}),
	Entry("a source", export.Source{ID: sourceID, Name: "db-2/mongod", CreatedAt: instant(1, 0)}),
	Entry("an event with every field set", export.Event{
		ID: eventID, TimelineID: timelineID, Seq: new(int64(7)), OccurredAt: instant(3, 14), OccurredOffset: new(7200),
		Precision: timeline.PrecisionMinute, Approximate: true, OccurredRaw: new("05:14"), EndedAt: new(instant(3, 20)),
		Source: new("db-2/mongod"), Tags: []string{"phase:detect"}, Text: "Primary stepped down",
		URL: new("https://x.test/1"), Author: new("julian"), CreatedAt: instant(4, 0), UpdatedAt: instant(4, 5),
	}),
	Entry("an event with every nullable field null", export.Event{
		ID: eventID, TimelineID: timelineID, OccurredAt: instant(3, 14), Precision: timeline.PrecisionSecond,
		Tags: []string{}, Text: "Primary stepped down", CreatedAt: instant(4, 0), UpdatedAt: instant(4, 0),
	}),
	Entry("an annotation on an event", export.Annotation{
		ID: annotationID, TimelineID: timelineID, EventID: new(eventID), Kind: timeline.AnnotationEvidence,
		Body: "rs.status() showed no primary", Author: new("julian"), Source: new("db-2/mongod"),
		URL: new("https://x.test/2"), CreatedAt: instant(4, 0), UpdatedAt: instant(4, 5),
	}),
	Entry("an annotation on the whole timeline with every nullable field null", export.Annotation{
		ID: annotationID, TimelineID: timelineID, Kind: timeline.AnnotationComment, Body: "Failover took too long",
		CreatedAt: instant(4, 0), UpdatedAt: instant(4, 0),
	}),
	Entry("an attachment kept with an event", export.Attachment{
		ID: attachmentID, EventID: new(eventID), Filename: "p99.png", MIME: "image/png",
		SHA256: strings.Repeat("ab", 32), Size: 2048, CreatedAt: instant(4, 0),
	}),
	Entry("an attachment kept with an annotation", sampleAttachment()),
)

var _ = Describe("Timeline.Domain", func() {
	It("says the timeline was last updated when it was created", func() {
		t := export.Timeline{ID: timelineID, Name: "mongo-outage", CreatedAt: instant(1, 0)}.Domain()

		Expect(t.UpdatedAt).To(Equal(at(1, 0)))
	})
})
