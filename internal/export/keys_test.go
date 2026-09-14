package export_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

// encodedKeys returns the keys json.Marshal writes for v, in order.
func encodedKeys(v any) []string {
	GinkgoHelper()
	data, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	dec := jsontext.NewDecoder(bytes.NewReader(data))
	_, err = dec.ReadToken()
	Expect(err).NotTo(HaveOccurred())
	var keys []string
	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		Expect(err).NotTo(HaveOccurred())
		keys = append(keys, name.String())
		Expect(dec.SkipValue()).To(Succeed())
	}

	return keys
}

// fromZero returns the keys DifferingKeys finds between populated and its type's zero value, and
// the keys populated encodes.
func fromZero[T export.Record](populated T) ([]string, []string) {
	var zero T
	return export.DifferingKeys(zero, populated), encodedKeys(populated)
}

func populatedEvent() export.Event {
	return export.Event{
		ID: eventID, TimelineID: timelineID, Seq: new(int64(7)), OccurredAt: instant(3, 14), OccurredOffset: new(7200),
		Precision: timeline.PrecisionMinute, Approximate: true, OccurredRaw: new("05:14"), EndedAt: new(instant(3, 20)),
		Source: new("db-2/mongod"), Tags: []string{"phase:detect"}, Text: "Primary stepped down",
		URL: new("https://x.test/1"), Author: new("julian"), CreatedAt: instant(4, 0), UpdatedAt: instant(4, 5),
	}
}

var _ = Describe("DifferingKeys", func() {
	DescribeTable("lists every key a record encodes when compared with its zero value, in order",
		func(keys func() ([]string, []string)) {
			differing, encoded := keys()
			Expect(differing).To(Equal(encoded))
		},
		Entry("a timeline", func() ([]string, []string) {
			return fromZero(export.Timeline{
				ID: timelineID, Name: "mongo-outage", Title: "Mongo outage", Refs: []string{"INC-4821"}, CreatedAt: instant(1, 0),
			})
		}),
		Entry("a source", func() ([]string, []string) {
			return fromZero(export.Source{ID: sourceID, Name: "db-2/mongod", CreatedAt: instant(1, 0)})
		}),
		Entry("an event", func() ([]string, []string) { return fromZero(populatedEvent()) }),
		Entry("an annotation", func() ([]string, []string) {
			return fromZero(export.Annotation{
				ID: annotationID, TimelineID: timelineID, EventID: new(eventID), Kind: timeline.AnnotationEvidence,
				Body: "rs.status() showed no primary", Author: new("julian"), Source: new("db-2/mongod"),
				URL: new("https://x.test/2"), CreatedAt: instant(4, 0), UpdatedAt: instant(4, 5),
			})
		}),
		Entry("an attachment", func() ([]string, []string) {
			return fromZero(export.Attachment{
				ID: attachmentID, EventID: new(eventID), AnnotationID: new(annotationID), Filename: "p99.png", MIME: "image/png",
				SHA256: strings.Repeat("ab", 32), Size: 2048, CreatedAt: instant(4, 0),
			})
		}),
	)

	It("lists nothing for two equal records", func() {
		Expect(export.DifferingKeys(populatedEvent(), populatedEvent())).To(BeEmpty())
	})
})
