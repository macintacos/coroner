package export_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing/iotest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

const (
	timelineID = "01a09862-b1c0-7000-8000-000000000001"
	sourceID   = "01a09862-b1c0-7000-8000-000000000002"
	eventID    = "01a09862-b1c0-7000-8000-000000000003"
	eventID2   = "01a09862-b1c0-7000-8000-000000000004"
	noteID     = "01a09862-b1c0-7000-8000-000000000005"
)

// sampleExport returns the JSONL export of a small timeline: a header, a timeline, a source,
// two events and an annotation on the first.
func sampleExport() string {
	env := export.NewEnvelope("0.1.0", at(9, 0))
	env.Query.Timelines = []string{"mongo-outage"}
	env.Timelines = []export.Timeline{{ID: timelineID, Name: "mongo-outage", Refs: []string{"INC-4821"}, CreatedAt: instant(1, 0)}}
	env.Sources = []export.Source{{ID: sourceID, Name: "db-2/mongod", CreatedAt: instant(1, 0)}}
	env.Events = []export.Event{sampleEvent(eventID, 1), sampleEvent(eventID2, 2)}
	env.Annotations = []export.Annotation{{
		ID: noteID, TimelineID: timelineID, EventID: new(eventID), Kind: timeline.AnnotationEvidence,
		Body: "rs.status() showed no primary", Source: new("db-2/mongod"), CreatedAt: instant(4, 0), UpdatedAt: instant(4, 0),
	}}

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

// eventLine returns an event's JSONL line after edit changes its decoded keys.
func eventLine(edit func(map[string]any)) string {
	var b bytes.Buffer
	Expect(export.WriteRecords(&b, []export.Record{sampleEvent(eventID, 1)})).To(Succeed())
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
			"header", "timeline " + timelineID, "source " + sourceID, "event " + eventID, "annotation " + noteID, "event " + eventID2,
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

		records, err := export.Decode(strings.NewReader(withColour + `{"type":"attachment","id":"x"}` + "\n"))

		Expect(err).NotTo(HaveOccurred())
		Expect(records).To(Equal([]export.Record{sampleEvent(eventID, 1)}))
	})

	It("rejects a header of an unsupported major version, naming both versions, and skips its records", func() {
		v2 := headerLine(func(h, _ map[string]any) { h["format_version"] = "2.0" })
		keep := func(map[string]any) {}
		v1, _, _ := strings.Cut(sampleExport(), "\n")

		records, err := export.Decode(strings.NewReader(v2 + eventLine(keep) + v1 + "\n" + eventLine(keep)))

		Expect(labels(records)).To(Equal([]string{"header", "event " + eventID}))
		decodeErr, ok := errors.AsType[*export.DecodeError](err)
		Expect(ok).To(BeTrue(), "a *DecodeError")
		Expect(decodeErr.Lines).To(ConsistOf(SatisfyAll(
			HaveField("Line", 1), HaveField("Err", MatchError(MatchRegexp(`"2\.0".*1\.`))))))
	})

	It("rejects a header whose query lacks a key", func() {
		partial := headerLine(func(_, q map[string]any) { delete(q, "order") })

		_, err := export.Decode(strings.NewReader(partial))

		Expect(err).To(MatchError(And(ContainSubstring("line 1"), ContainSubstring(`"order"`))))
	})

	It("rejects a header whose format is not coroner.export", func() {
		other := headerLine(func(h, _ map[string]any) { h["format"] = "grafana.annotations" })

		_, err := export.Decode(strings.NewReader(other))

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
				HaveField("Line", 2), HaveField("Err", MatchError(ContainSubstring(reason))))))
		},
		Entry("a missing key", func() string { return eventLine(func(m map[string]any) { delete(m, "seq") }) }, `"seq"`),
		Entry("null for a key that is never null", func() string { return eventLine(func(m map[string]any) { m["text"] = nil }) }, `"text"`),
		Entry("a value of the wrong JSON type", func() string { return eventLine(func(m map[string]any) { m["seq"] = "one" }) }, "seq"),
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
		Entry("a missing type", func() string { return eventLine(func(m map[string]any) { delete(m, "type") }) }, `"type"`),
		Entry("a line that is not an object", func() string { return "[1,2]\n" }, "array"),
		Entry("a duplicate key", func() string { return `{"type":"event","type":"event"}` + "\n" }, "duplicate"),
	)

	It("groups many invalid lines under one count, listing the first five", func() {
		records, err := export.Decode(strings.NewReader(strings.Repeat("{}\n", 7)))

		Expect(records).To(BeEmpty())
		message := err.Error()
		Expect(strings.Split(message, "\n")).To(HaveLen(7))
		Expect(message).To(SatisfyAll(
			ContainSubstring("7"), ContainSubstring("line 5:"), Not(ContainSubstring("line 6:")), HaveSuffix("and 2 more")))
	})

	It("returns the error of a reader that fails", func() {
		gone := errors.New("disk gone")

		_, err := export.Decode(iotest.ErrReader(gone))

		Expect(err).To(MatchError(gone))
	})
})
