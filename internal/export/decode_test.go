package export_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing/iotest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	timelineID   = "01a09862-b1c0-7000-8000-000000000001"
	sourceID     = "01a09862-b1c0-7000-8000-000000000002"
	eventID      = "01a09862-b1c0-7000-8000-000000000003"
	eventID2     = "01a09862-b1c0-7000-8000-000000000004"
	annotationID = "01a09862-b1c0-7000-8000-000000000005"
	attachmentID = "01a09862-b1c0-7000-8000-000000000006"
	linkID       = "01a09862-b1c0-7000-8000-000000000007"
)

// sampleExport returns the JSONL export of a small timeline: a header, a timeline, a source,
// two events, an annotation on the first, an attachment on that annotation and a link from the
// first event to the second.
func sampleExport() string {
	env := export.NewEnvelope("0.1.0", at(9, 0))
	env.Query.Timelines = []string{"mongo-outage"}
	env.Timelines = []export.Timeline{{ID: timelineID, Name: "mongo-outage", Refs: []string{"INC-4821"}, CreatedAt: instant(1, 0)}}
	env.Sources = []export.Source{{ID: sourceID, Name: "db-2/mongod", CreatedAt: instant(1, 0)}}
	env.Events = []export.Event{sampleEvent(eventID, 1), sampleEvent(eventID2, 2)}
	env.Annotations = []export.Annotation{{
		ID: annotationID, TimelineID: timelineID, EventID: new(eventID), Kind: timeline.AnnotationEvidence,
		Body: "rs.status() showed no primary", Source: new("db-2/mongod"), CreatedAt: instant(4, 0), UpdatedAt: instant(4, 0),
	}}
	env.Attachments = []export.Attachment{sampleAttachment()}
	env.Links = []export.Link{sampleLink()}

	records, err := export.Records(env)
	Expect(err).NotTo(HaveOccurred())
	var b bytes.Buffer
	Expect(export.WriteRecords(&b, records)).To(Succeed())

	return b.String()
}

func sampleEvent(id string, seq int64) export.Event {
	return export.Event{
		ID: id, TimelineID: timelineID, Seq: &seq, OccurredAt: instant(3, int(seq)), Precision: timeline.PrecisionSecond,
		Source: new("db-2/mongod"), Tags: []string{"phase:detect"}, Text: "Primary stepped down",
		CreatedAt: instant(4, 0), UpdatedAt: instant(4, 0),
	}
}

func sampleAttachment() export.Attachment {
	return export.Attachment{
		ID: attachmentID, AnnotationID: new(annotationID), Filename: "rs-status.txt", MIME: "text/plain; charset=utf-8",
		SHA256: strings.Repeat("0f", 32), Size: 2048, CreatedAt: instant(4, 0),
	}
}

func sampleLink() export.Link {
	return export.Link{
		ID: linkID, FromEventID: eventID, ToEventID: eventID2, Kind: timeline.LinkLedTo, CreatedAt: instant(5, 0), Author: new("julian"),
	}
}

// eventLine returns an event's JSONL line after edit changes its decoded keys.
func eventLine(edit func(map[string]any)) string {
	return recordLine(sampleEvent(eventID, 1), edit)
}

// attachmentLine returns an attachment's JSONL line after edit changes its decoded keys.
func attachmentLine(edit func(map[string]any)) string {
	return recordLine(sampleAttachment(), edit)
}

func recordLine(record export.Record, edit func(map[string]any)) string {
	var b bytes.Buffer
	Expect(export.WriteRecords(&b, []export.Record{record})).To(Succeed())
	var members map[string]any
	Expect(json.Unmarshal(b.Bytes(), &members)).To(Succeed())
	edit(members)
	line, err := json.Marshal(members)
	Expect(err).NotTo(HaveOccurred())

	return string(line) + "\n"
}

func headerLine(edit func(header, query map[string]any)) string {
	first, _, _ := strings.Cut(sampleExport(), "\n")
	var members map[string]any
	Expect(json.Unmarshal([]byte(first), &members)).To(Succeed())
	query, ok := members["query"].(map[string]any)
	Expect(ok).To(BeTrue())
	edit(members, query)
	line, err := json.Marshal(members)
	Expect(err).NotTo(HaveOccurred())

	return string(line) + "\n"
}

func encodeRecords(records []export.Record) string {
	var b bytes.Buffer
	Expect(export.WriteRecords(&b, records)).To(Succeed())

	return b.String()
}

var _ = Describe("Decode", func() {
	It("reads an export back into records that encode to the same bytes", func() {
		records, err := export.Decode(strings.NewReader(sampleExport()))

		Expect(err).NotTo(HaveOccurred())
		Expect(encodeRecords(records)).To(Equal(sampleExport()))
	})

	It("reads two concatenated exports, each with its header", func() {
		records, err := export.Decode(strings.NewReader(sampleExport() + sampleExport()))

		Expect(err).NotTo(HaveOccurred())
		one := []string{
			"header", "timeline " + timelineID, "source " + sourceID, "event " + eventID, "annotation " + annotationID, "attachment " + attachmentID,
			"event " + eventID2, "link " + linkID,
		}
		Expect(labels(records)).To(Equal(append(one, one...)))
	})

	It("reads events with no header", func() {
		keep := func(map[string]any) {}

		records, err := export.Decode(strings.NewReader(eventLine(keep) + eventLine(keep)))

		Expect(err).NotTo(HaveOccurred())
		Expect(labels(records)).To(Equal([]string{"event " + eventID, "event " + eventID}))
	})

	It("ignores a key and a record type it does not know", func() {
		withColour := eventLine(func(m map[string]any) { m["colour"] = "red" })

		records, err := export.Decode(strings.NewReader(withColour + `{"type":"reaction","id":"x"}` + "\n"))

		Expect(err).NotTo(HaveOccurred())
		Expect(records).To(Equal([]export.Record{sampleEvent(eventID, 1)}))
	})

	DescribeTable("rejects a header of an unsupported format_version, naming both versions, and skips its records",
		func(version string, dropKeys ...string) {
			unsupported := headerLine(func(h, _ map[string]any) {
				h["format_version"] = version
				for _, key := range dropKeys {
					delete(h, key)
				}
			})
			keep := func(map[string]any) {}
			v1, _, _ := strings.Cut(sampleExport(), "\n")

			records, err := export.Decode(strings.NewReader(unsupported + eventLine(keep) + v1 + "\n" + eventLine(keep)))

			Expect(labels(records)).To(Equal([]string{"header", "event " + eventID}))
			decodeErr, ok := errors.AsType[*export.DecodeError](err)
			Expect(ok).To(BeTrue(), "a *DecodeError")
			Expect(decodeErr.Lines).To(ConsistOf(SatisfyAll(
				HaveField("Line", 1), HaveField("Err", MatchError(MatchRegexp(regexp.QuoteMeta(strconv.Quote(version))+`.*1\.`))))))
		},
		Entry("another major version", "2.0"),
		Entry("another major version whose other keys differ", "2.0", "query"),
		Entry("no minor version", "1"),
		Entry("a minor version that is not a number", "1.banana"),
	)

	It("rejects a header whose query lacks a key", func() {
		partial := headerLine(func(_, q map[string]any) { delete(q, "order") })

		_, err := export.Decode(strings.NewReader(partial))

		Expect(err).To(MatchError(And(ContainSubstring("line 1"), ContainSubstring(`"order"`))))
	})

	It("reads the records after a header that is invalid but of a supported version", func() {
		badTag := headerLine(func(_, q map[string]any) { q["tags"] = []string{"Bad"} })
		badID := eventLine(func(m map[string]any) { m["id"] = "e1" })

		records, err := export.Decode(strings.NewReader(badTag + eventLine(func(map[string]any) {}) + badID))

		Expect(labels(records)).To(Equal([]string{"event " + eventID}))
		decodeErr, ok := errors.AsType[*export.DecodeError](err)
		Expect(ok).To(BeTrue(), "a *DecodeError")
		Expect(decodeErr.Lines).To(HaveExactElements(HaveField("Line", 1), HaveField("Line", 3)))
	})

	It("rejects a header whose format is not coroner.export, and skips its records", func() {
		other := headerLine(func(h, _ map[string]any) { h["format"] = "grafana.annotations" })

		records, err := export.Decode(strings.NewReader(other + eventLine(func(map[string]any) {})))

		Expect(records).To(BeEmpty())
		Expect(err).To(MatchError(ContainSubstring("grafana.annotations")))
	})

	It("reads a single line of more than a mebibyte", func() {
		long := strings.Repeat("a", 1<<20)

		records, err := export.Decode(strings.NewReader(eventLine(func(m map[string]any) { m["text"] = long })))

		Expect(err).NotTo(HaveOccurred())
		Expect(records).To(ConsistOf(HaveField("Text", long)))
	})

	It("reads a last line that has no newline", func() {
		line := eventLine(func(map[string]any) {})

		records, err := export.Decode(strings.NewReader(strings.TrimSuffix(line, "\n")))

		Expect(err).NotTo(HaveOccurred())
		Expect(records).To(HaveLen(1))
	})

	DescribeTable("reports an invalid line by number and still reads the lines around it",
		func(bad func() string, reason string) {
			good := eventLine(func(map[string]any) {})

			records, err := export.Decode(strings.NewReader(good + bad() + good))

			Expect(records).To(HaveLen(2))
			decodeErr, ok := errors.AsType[*export.DecodeError](err)
			Expect(ok).To(BeTrue(), "a *DecodeError")
			Expect(decodeErr.Lines).To(ConsistOf(SatisfyAll(
				HaveField("Line", 2), HaveField("Err", MatchError(SatisfyAll(ContainSubstring(reason), Not(ContainSubstring(" Go "))))))))
		},
		Entry("a missing key", func() string { return eventLine(func(m map[string]any) { delete(m, "seq") }) }, `"seq"`),
		Entry("null for a key that is never null", func() string { return eventLine(func(m map[string]any) { m["text"] = nil }) }, `"text"`),
		Entry("a value of the wrong JSON type", func() string { return eventLine(func(m map[string]any) { m["seq"] = "one" }) }, `key "seq"`),
		Entry("a list item of the wrong JSON type", func() string { return eventLine(func(m map[string]any) { m["tags"] = []int{1} }) }, "tags"),
		Entry("a query value of the wrong JSON type",
			func() string { return headerLine(func(_, q map[string]any) { q["tags"] = "phase:detect" }) }, "tags"),
		Entry("an instant not in the canonical spelling",
			func() string { return eventLine(func(m map[string]any) { m["occurred_at"] = "2026-09-11T03:01:00Z" }) },
			"2026-09-11T03:01:00Z"),
		Entry("an id that is not a UUID", func() string { return eventLine(func(m map[string]any) { m["id"] = "e1" }) }, `"e1"`),
		Entry("an id in upper case",
			func() string {
				return eventLine(func(m map[string]any) { m["timeline_id"] = strings.ToUpper(timelineID) })
			},
			strings.ToUpper(timelineID)),
		Entry("an invalid source name", func() string { return eventLine(func(m map[string]any) { m["source"] = "DB 2" }) }, "DB 2"),
		Entry("an invalid tag", func() string { return eventLine(func(m map[string]any) { m["tags"] = []string{"Election"} }) }, "Election"),
		Entry("an invalid timeline name",
			func() string {
				return `{"type":"timeline","id":"` + timelineID + `","name":"Mongo Outage","title":"","refs":[],"created_at":"2026-09-11T01:00:00.000000000Z"}` + "\n"
			},
			"Mongo Outage"),
		Entry("an attachment missing a key",
			func() string { return attachmentLine(func(m map[string]any) { delete(m, "sha256") }) }, `"sha256"`),
		Entry("an attachment whose sha256 is not 64 lowercase hex digits",
			func() string {
				return attachmentLine(func(m map[string]any) { m["sha256"] = strings.Repeat("0F", 32) })
			},
			strings.Repeat("0F", 32)),
		Entry("an attachment on both an event and an annotation",
			func() string { return attachmentLine(func(m map[string]any) { m["event_id"] = eventID }) }, "exactly one"),
		Entry("an attachment on nothing",
			func() string { return attachmentLine(func(m map[string]any) { m["annotation_id"] = nil }) }, "exactly one"),
		Entry("an attachment whose owner id is not a UUID",
			func() string { return attachmentLine(func(m map[string]any) { m["annotation_id"] = "a1" }) }, `"a1"`),
		Entry("an attachment with a negative size",
			func() string { return attachmentLine(func(m map[string]any) { m["size"] = -1 }) }, "size -1"),
		Entry("a link missing a key",
			func() string { return recordLine(sampleLink(), func(m map[string]any) { delete(m, "to_event_id") }) }, `"to_event_id"`),
		Entry("a link whose event id is not a UUID",
			func() string { return recordLine(sampleLink(), func(m map[string]any) { m["from_event_id"] = "e1" }) }, `"e1"`),
		Entry("a missing type", func() string { return eventLine(func(m map[string]any) { delete(m, "type") }) }, `"type"`),
		Entry("a type that is not a string", func() string { return eventLine(func(m map[string]any) { m["type"] = 1 }) }, `key "type"`),
		Entry("a line that is not an object", func() string { return "[1,2]\n" }, "not a JSON object"),
		Entry("a blank line", func() string { return " \n" }, "blank line"),
		Entry("a duplicate key", func() string { return `{"type":"event","type":"event"}` + "\n" }, "duplicate"),
	)

	It("lists every reason a line is invalid on that line's one entry", func() {
		bad := eventLine(func(m map[string]any) {
			m["id"] = strings.ToUpper(eventID)
			m["source"] = "DB 2"
		})

		_, err := export.Decode(strings.NewReader(bad))

		Expect(err).To(HaveOccurred())
		Expect(strings.Split(err.Error(), "\n")).To(HaveExactElements(
			HavePrefix("1 line "),
			SatisfyAll(HavePrefix("line 1: "), ContainSubstring(strings.ToUpper(eventID)), ContainSubstring("DB 2"))))
	})

	It("groups many invalid lines under one count, listing the first five", func() {
		records, err := export.Decode(strings.NewReader(strings.Repeat("{}\n", 7)))

		Expect(records).To(BeEmpty())
		message := err.Error()
		Expect(strings.Split(message, "\n")).To(HaveLen(7))
		Expect(message).To(SatisfyAll(
			HavePrefix("7 "), ContainSubstring("line 5:"), Not(ContainSubstring("line 6:")), HaveSuffix("and 2 more")))
	})

	It("returns the error of a reader that fails", func() {
		gone := errors.New("disk gone")

		_, err := export.Decode(iotest.ErrReader(gone))

		Expect(err).To(MatchError(gone))
	})
})
