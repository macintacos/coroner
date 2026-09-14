package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("planImport", func() {
	var (
		tl         export.Timeline
		stepdown   export.Event
		evidence   export.Annotation
		mongod     export.Source
		screenshot export.Attachment
		content    []byte
	)

	BeforeEach(func() {
		tl = planTimeline(1, "inc-4821")
		mongod = export.Source{ID: planID(2), Name: "mongod", CreatedAt: planInstant(0)}
		stepdown = planEvent(3, tl.ID, "Primary stepped down")
		evidence = planAnnotation(4, tl.ID, &stepdown.ID)
		content = []byte("rs.status() output")
		screenshot = planAttachment(5, content)
		screenshot.EventID = &stepdown.ID
	})

	all := func() []export.Record { return []export.Record{tl, mongod, stepdown, evidence, screenshot} }

	It("inserts every record into an empty database and counts each type", func() {
		in := planInput(all(), storedRecords{})
		in.sidecars = map[string][]byte{screenshot.ID: content}

		plan, err := planImport(in)

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.blocked).NotTo(HaveOccurred())
		Expect(plan.result.Timelines).To(Equal(importCounts{Inserted: 1}))
		Expect(plan.result.Sources).To(Equal(importCounts{Inserted: 1}))
		Expect(plan.result.Events).To(Equal(importCounts{Inserted: 1}))
		Expect(plan.result.Annotations).To(Equal(importCounts{Inserted: 1}))
		Expect(plan.result.Attachments).To(Equal(importCounts{Inserted: 1}))
		Expect(plan.write.Timelines).To(Equal([]timeline.Timeline{tl.Domain()}))
		Expect(plan.write.Sources).To(Equal([]timeline.Source{mongod.Domain()}))
		Expect(plan.write.Events).To(Equal([]timeline.Event{stepdown.Domain()}))
		Expect(plan.write.Annotations).To(Equal([]timeline.Annotation{evidence.Domain()}))
		Expect(plan.write.Attachments).To(HaveLen(1))
		Expect(plan.write.Attachments[0].Attachment).To(Equal(screenshot.Domain()))
		Expect(io.ReadAll(plan.write.Attachments[0].Content)).To(Equal(content))
	})

	It("skips every record the database already holds unchanged, and writes nothing", func() {
		plan, err := planImport(planInput(all(), storedFrom(tl, mongod, stepdown, evidence, screenshot)))

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.blocked).NotTo(HaveOccurred())
		Expect(plan.result.Events).To(Equal(importCounts{Identical: 1}))
		Expect(plan.result.Attachments).To(Equal(importCounts{Identical: 1}))
		Expect(plan.result.Conflicts).To(BeEmpty())
		Expect(plan.write).To(Equal(store.ImportRecords{}))
	})

	It("ignores seq when comparing events, since each database numbers its own", func() {
		stored := stepdown
		stored.Seq = new(int64(9))

		plan, err := planImport(planInput([]export.Record{tl, stepdown}, storedFrom(tl, stored)))

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.result.Events).To(Equal(importCounts{Identical: 1}))
	})

	Describe("a record stored with different content", func() {
		var edited export.Event

		BeforeEach(func() {
			edited = stepdown
			edited.Text = "Primary stepped down after an election timeout"
			edited.Tags = []string{"election"}
		})

		It("blocks the import as id_conflict under fail, naming the keys that differ", func() {
			plan, err := planImport(planInput([]export.Record{tl, edited}, storedFrom(tl, stepdown)))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.blocked).To(HaveField("Kind", KindIDConflict))
			Expect(plan.result.Events).To(Equal(importCounts{Conflicting: 1}))
			Expect(plan.result.Conflicts).To(Equal([]importConflict{{Type: "event", ID: stepdown.ID, Fields: []string{"tags", "text"}}}))
		})

		It("keeps the stored record under skip", func() {
			in := planInput([]export.Record{tl, edited}, storedFrom(tl, stepdown))
			in.onConflict = onConflictSkip

			plan, err := planImport(in)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.blocked).NotTo(HaveOccurred())
			Expect(plan.result.Conflicts).To(HaveLen(1))
			Expect(plan.write).To(Equal(store.ImportRecords{}))
		})

		It("replaces the stored record under replace", func() {
			in := planInput([]export.Record{tl, edited}, storedFrom(tl, stepdown))
			in.onConflict = onConflictReplace

			plan, err := planImport(in)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.blocked).NotTo(HaveOccurred())
			Expect(plan.write.EventReplacements).To(Equal([]timeline.Event{edited.Domain()}))
			Expect(plan.write.Events).To(BeEmpty())
		})

		It("gives a replaced timeline the importer's clock as its updated at, which exports do not carry", func() {
			retitled := tl
			retitled.Title = "Mongo primary election"
			in := planInput([]export.Record{retitled}, storedFrom(tl))
			in.onConflict = onConflictReplace

			plan, err := planImport(in)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.write.TimelineReplacements).To(HaveLen(1))
			Expect(plan.write.TimelineReplacements[0].Title).To(Equal("Mongo primary election"))
			Expect(plan.write.TimelineReplacements[0].UpdatedAt).To(Equal(in.now))
		})

		DescribeTable("blocks replace when the record cannot be overwritten, and keeps it under skip",
			func(stored, incoming func() []export.Record) {
				in := planInput(incoming(), storedFrom(stored()...))
				in.onConflict = onConflictReplace

				plan, err := planImport(in)

				Expect(err).NotTo(HaveOccurred())
				Expect(plan.blocked).To(HaveField("Kind", KindIDConflict))

				in.onConflict = onConflictSkip
				plan, err = planImport(in)
				Expect(err).NotTo(HaveOccurred())
				Expect(plan.blocked).NotTo(HaveOccurred())
			},
			Entry("an event moved to another timeline",
				func() []export.Record { return []export.Record{tl, planTimeline(6, "other"), stepdown} },
				func() []export.Record {
					moved := stepdown
					moved.TimelineID = planID(6)
					return []export.Record{moved}
				}),
			Entry("an attachment, which never changes",
				func() []export.Record { return []export.Record{tl, stepdown, screenshot} },
				func() []export.Record {
					renamed := screenshot
					renamed.Filename = "renamed.png"
					return []export.Record{renamed}
				}),
		)
	})

	It("rejects a timeline whose name a stored timeline with another id holds, as timeline_exists", func() {
		_, err := planImport(planInput([]export.Record{tl}, storedFrom(planTimeline(6, tl.Name))))

		Expect(err).To(HaveField("Kind", KindTimelineExists))
	})

	DescribeTable("rejects input it cannot import as invalid_input, naming the record",
		func(records func() []export.Record, prepare func(*importInput), wantInMessage string) {
			in := planInput(records(), storedRecords{})
			if prepare != nil {
				prepare(&in)
			}

			_, err := planImport(in)

			Expect(err).To(And(HaveField("Kind", KindInvalidInput), MatchError(ContainSubstring(wantInMessage))))
		},
		Entry("an event whose timeline is neither in the input nor stored",
			func() []export.Record { return []export.Record{stepdown} }, nil, planID(3)),
		Entry("an annotation whose event is neither in the input nor stored",
			func() []export.Record { return []export.Record{tl, evidence} }, nil, planID(4)),
		Entry("an annotation on an event in another timeline",
			func() []export.Record {
				other := planTimeline(6, "other")
				misplaced := evidence
				misplaced.TimelineID = other.ID
				return []export.Record{tl, other, stepdown, misplaced}
			}, nil, planID(4)),
		Entry("an attachment whose owner is neither in the input nor stored",
			func() []export.Record { return []export.Record{screenshot} }, nil, planID(5)),
		Entry("one id twice with different content",
			func() []export.Record {
				edited := stepdown
				edited.Text = "edited"
				return []export.Record{tl, stepdown, edited}
			}, nil, planID(3)),
		Entry("two timelines with one name",
			func() []export.Record { return []export.Record{tl, planTimeline(6, tl.Name)} }, nil, "inc-4821"),
		Entry("a record that breaks a domain rule",
			func() []export.Record {
				blank := stepdown
				blank.Text = " "
				return []export.Record{tl, blank}
			}, nil, "text"),
		Entry("a record outside the timeline -t selects",
			func() []export.Record { return []export.Record{tl, stepdown} },
			func(in *importInput) {
				other := planTimeline(6, "other").Domain()
				in.guard = &other
				in.stored = storedFrom(planTimeline(6, "other"))
			}, planID(3)),
		Entry("an attachment file whose content does not match its sha256",
			func() []export.Record { return []export.Record{tl, stepdown, screenshot} },
			func(in *importInput) { in.sidecars = map[string][]byte{screenshot.ID: []byte("something else")} }, planID(5)),
	)

	It("lists the first five problems, then how many more there are", func() {
		records := []export.Record{}
		for n := range 7 {
			records = append(records, planEvent(10+n, tl.ID, "orphan"))
		}

		_, err := planImport(planInput(records, storedRecords{}))

		Expect(err).To(MatchError(And(HavePrefix("7 records cannot be imported:\n"), HaveSuffix("\nand 2 more"))))
	})

	Describe("sources", func() {
		It("matches a stored source by name, whatever its id", func() {
			renumbered := mongod
			renumbered.ID = planID(7)

			plan, err := planImport(planInput([]export.Record{tl, renumbered, stepdown}, storedFrom(tl, mongod)))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.result.Sources).To(Equal(importCounts{Identical: 1}))
			Expect(plan.write.Sources).To(BeEmpty())
		})

		It("gives a new id to a source whose id a differently named stored source holds", func() {
			renamed := mongod
			renamed.Name = "db-2/mongod"

			plan, err := planImport(planInput([]export.Record{renamed}, storedFrom(mongod)))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.write.Sources).To(HaveLen(1))
			Expect(plan.write.Sources[0].Name).To(Equal("db-2/mongod"))
			Expect(plan.write.Sources[0].ID).NotTo(Equal(mongod.ID))
		})

		It("creates a source an imported event names without a source record", func() {
			plan, err := planImport(planInput([]export.Record{tl, stepdown}, storedRecords{}))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.write.Sources).To(ConsistOf(HaveField("Name", "mongod")))
			Expect(plan.write.Sources[0].CreatedAt).To(Equal(time.Time(stepdown.CreatedAt)))
		})
	})

	Describe("attachments", func() {
		records := func() []export.Record { return []export.Record{tl, stepdown, screenshot} }

		It("imports one whose blob is already beside the database without its content", func() {
			in := planInput(records(), storedRecords{})
			in.localBlobs = map[string]bool{screenshot.SHA256: true}

			plan, err := planImport(in)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.write.Attachments).To(ConsistOf(HaveField("Content", BeNil())))
		})

		It("skips one whose file is neither beside the input nor in the database, and lists it", func() {
			plan, err := planImport(planInput(records(), storedRecords{}))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.blocked).NotTo(HaveOccurred())
			Expect(plan.write.Attachments).To(BeEmpty())
			Expect(plan.result.Attachments).To(Equal(importCounts{}))
			Expect(plan.result.MissingAttachments).To(Equal([]missingAttachment{{ID: screenshot.ID, SHA256: screenshot.SHA256}}))
		})
	})
})

func planID(n int) string {
	return fmt.Sprintf("01a09862-b1c0-7000-8000-%012d", n)
}

func planInstant(minute int) export.Instant {
	return export.Instant(time.Date(2026, 9, 11, 3, minute, 0, 0, time.UTC))
}

func planTimeline(n int, name string) export.Timeline {
	return export.Timeline{ID: planID(n), Name: name, Refs: []string{}, CreatedAt: planInstant(0)}
}

func planEvent(n int, timelineID, text string) export.Event {
	return export.Event{
		ID: planID(n), TimelineID: timelineID, Seq: new(int64(n)), OccurredAt: planInstant(14), Precision: timeline.PrecisionSecond,
		Source: new("mongod"), Tags: []string{}, Text: text, Author: new("julian"), CreatedAt: planInstant(20), UpdatedAt: planInstant(21),
	}
}

func planAnnotation(n int, timelineID string, eventID *string) export.Annotation {
	return export.Annotation{
		ID: planID(n), TimelineID: timelineID, EventID: eventID, Kind: timeline.AnnotationEvidence, Body: "replSetStepDown in the log",
		Author: new("julian"), CreatedAt: planInstant(22), UpdatedAt: planInstant(22),
	}
}

func planAttachment(n int, content []byte) export.Attachment {
	sum := sha256.Sum256(content)
	return export.Attachment{
		ID: planID(n), Filename: "rs-status.txt", MIME: "text/plain; charset=utf-8", SHA256: hex.EncodeToString(sum[:]),
		Size: int64(len(content)), CreatedAt: planInstant(23),
	}
}

// storedFrom returns the stored state a database holds after importing records.
func storedFrom(records ...export.Record) storedRecords {
	stored := storedRecords{
		timelines: map[string]timeline.Timeline{}, events: map[string]timeline.Event{},
		annotations: map[string]timeline.Annotation{}, attachments: map[string]timeline.Attachment{},
	}
	for _, r := range records {
		switch r := r.(type) {
		case export.Timeline:
			stored.timelines[r.ID] = r.Domain()
		case export.Source:
			stored.sources = append(stored.sources, r.Domain())
		case export.Event:
			stored.events[r.ID] = r.Domain()
		case export.Annotation:
			stored.annotations[r.ID] = r.Domain()
		case export.Attachment:
			stored.attachments[r.ID] = r.Domain()
		}
	}

	return stored
}

// planInput returns an import of records into stored under --on-conflict fail, with no attachment
// files, ids numbered from 100 and a fixed clock.
func planInput(records []export.Record, stored storedRecords) importInput {
	next := 100
	return importInput{
		records: records, stored: stored, onConflict: onConflictFail, now: time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC),
		newID: func() (uuid.UUID, error) {
			next++
			return uuid.Parse(planID(next))
		},
	}
}
