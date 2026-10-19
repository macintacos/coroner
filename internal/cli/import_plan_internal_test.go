package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
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
		pager      export.Event
		ledTo      export.Link
	)

	BeforeEach(func() {
		tl = planTimeline(1, "inc-4821")
		mongod = export.Source{ID: planID(2), Name: "mongod", CreatedAt: planInstant(0)}
		stepdown = planEvent(3, tl.ID, "Primary stepped down")
		evidence = planAnnotation(4, tl.ID, &stepdown.ID)
		content = []byte("rs.status() output")
		screenshot = planAttachment(5, content)
		screenshot.EventID = &stepdown.ID
		pager = planEvent(30, tl.ID, "Pager fires for db-2")
		ledTo = export.Link{
			ID: planID(31), FromEventID: stepdown.ID, ToEventID: pager.ID, Kind: timeline.LinkLedTo, CreatedAt: planInstant(24), Author: new("julian"),
		}
	})

	all := func() []export.Record { return []export.Record{tl, mongod, stepdown, evidence, screenshot} }

	It("inserts every record into an empty database and counts each type", func() {
		in := planInput(all(), storedRecords{})
		in.sidecars = map[string][]byte{screenshot.SHA256: content}

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

	It("accepts one event twice that differs only in seq, and plans it once", func() {
		renumbered := stepdown
		renumbered.Seq = new(int64(9))

		plan, err := planImport(planInput([]export.Record{tl, stepdown, renumbered}, storedRecords{}))

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.write.Events).To(Equal([]timeline.Event{stepdown.Domain()}))
	})

	It("compares tags sorted and once each, and refs once each, as the database stores them", func() {
		repeatedRefs, storedRefs := tl, tl
		repeatedRefs.Refs = []string{"OPS-2", "OPS-1", "OPS-2"}
		storedRefs.Refs = []string{"OPS-2", "OPS-1"}
		unsortedTags, storedTags := stepdown, stepdown
		unsortedTags.Tags = []string{"team:db", "phase:detect", "team:db"}
		storedTags.Tags = []string{"phase:detect", "team:db"}

		plan, err := planImport(planInput([]export.Record{repeatedRefs, unsortedTags}, storedFrom(storedRefs, storedTags)))

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.result.Conflicts).To(BeEmpty())
		Expect(plan.result.Timelines).To(Equal(importCounts{Identical: 1}))
		Expect(plan.result.Events).To(Equal(importCounts{Identical: 1}))
	})

	It("names the timelines it writes to, sorted, and not those it only finds unchanged", func() {
		unchanged := planTimeline(6, "z-unchanged")
		unchangedEvent := planEvent(7, unchanged.ID, "Secondary caught up")
		shots := planTimeline(8, "b-shots")
		shotEvent := planEvent(9, shots.ID, "Dashboard went red")
		lagging := planTimeline(10, "a-lagging")
		screenshot.EventID = &shotEvent.ID
		in := planInput(
			[]export.Record{tl, unchangedEvent, planEvent(11, lagging.ID, "Secondary lagged"), screenshot},
			storedFrom(tl, unchanged, unchangedEvent, shots, shotEvent, lagging),
		)
		in.localBlobs = map[string]bool{screenshot.SHA256: true}

		plan, err := planImport(in)

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.timelineNames).To(Equal([]string{"a-lagging", "b-shots", "inc-4821"}))
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

		It("replaces a stored annotation whose body differs under replace", func() {
			reworded := evidence
			reworded.Body = "replSetStepDown and an election in the log"
			in := planInput([]export.Record{tl, stepdown, reworded}, storedFrom(tl, stepdown, evidence))
			in.onConflict = onConflictReplace

			plan, err := planImport(in)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.blocked).NotTo(HaveOccurred())
			Expect(plan.result.Conflicts).To(Equal([]importConflict{{Type: "annotation", ID: evidence.ID, Fields: []string{"body"}}}))
			Expect(plan.write.AnnotationReplacements).To(Equal([]timeline.Annotation{reworded.Domain()}))
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
			Entry("a link, which never changes",
				func() []export.Record { return []export.Record{tl, stepdown, pager, ledTo} },
				func() []export.Record {
					reauthored := ledTo
					reauthored.Author = new("sam")
					return []export.Record{reauthored}
				}),
			Entry("a link of one kind between the same events under another id",
				func() []export.Record { return []export.Record{tl, stepdown, pager, ledTo} },
				func() []export.Record {
					twin := ledTo
					twin.ID = planID(32)
					return []export.Record{twin}
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
		Entry("an attachment whose stored owner is outside the timeline -t selects",
			func() []export.Record { return []export.Record{screenshot} },
			func(in *importInput) {
				other := planTimeline(6, "other")
				in.stored = storedFrom(tl, other, stepdown)
				guard := other.Domain()
				in.guard = &guard
			}, planID(5)),
		Entry("an annotation on a stored event that the import would move to another timeline",
			func() []export.Record {
				other := planTimeline(6, "other")
				moved, misplaced := stepdown, evidence
				moved.TimelineID, misplaced.TimelineID = other.ID, other.ID
				return []export.Record{other, moved, misplaced}
			},
			func(in *importInput) {
				in.stored = storedFrom(tl, planTimeline(6, "other"), stepdown)
				in.onConflict = onConflictSkip
			}, planID(4)),
		Entry("an event whose occurred_offset is more than a day",
			func() []export.Record {
				far := stepdown
				far.OccurredOffset = new(86401)
				return []export.Record{tl, far}
			}, nil, "occurred offset"),
		Entry("an event whose seq leaves its timeline no room to number more events",
			func() []export.Record {
				huge := stepdown
				huge.Seq = new(int64(math.MaxInt64/2 + 1))
				return []export.Record{tl, huge}
			}, nil, "seq"),
		Entry("an attachment file whose content does not match its sha256",
			func() []export.Record { return []export.Record{tl, stepdown, screenshot} },
			func(in *importInput) { in.sidecars = map[string][]byte{screenshot.SHA256: []byte("something else")} }, planID(5)),
		Entry("a link whose event is neither in the input nor stored",
			func() []export.Record { return []export.Record{tl, stepdown, ledTo} }, nil, planID(30)),
		Entry("a link from an event to itself",
			func() []export.Record {
				self := ledTo
				self.ToEventID = stepdown.ID
				return []export.Record{tl, stepdown, self}
			}, nil, planID(31)),
		Entry("a link of a kind this coroner does not know",
			func() []export.Record {
				after := ledTo
				after.Kind = "after"
				return []export.Record{tl, stepdown, pager, after}
			}, nil, "after"),
		Entry("one link of a kind between two events under two ids",
			func() []export.Record {
				twin := ledTo
				twin.ID = planID(32)
				return []export.Record{tl, stepdown, pager, ledTo, twin}
			}, nil, "led_to from "+planID(3)+" to "+planID(30)),
		Entry("a link to an event outside the timeline -t selects",
			func() []export.Record {
				checkout := planTimeline(33, "checkout-latency")
				p99 := planEvent(34, checkout.ID, "Checkout p99 above 2s")
				across := ledTo
				across.ToEventID = p99.ID
				return []export.Record{tl, checkout, stepdown, p99, across}
			},
			func(in *importInput) {
				guard := tl.Domain()
				in.guard = &guard
				in.stored = storedFrom(tl)
			}, planID(31)),
	)

	Describe("links", func() {
		It("inserts a link between two timelines' events with the events", func() {
			checkout := planTimeline(33, "checkout-latency")
			p99 := planEvent(34, checkout.ID, "Checkout p99 above 2s")
			ledTo.ToEventID = p99.ID

			plan, err := planImport(planInput([]export.Record{tl, checkout, stepdown, p99, ledTo}, storedRecords{}))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.result.Links).To(Equal(importCounts{Inserted: 1}))
			Expect(plan.write.Links).To(Equal([]timeline.Link{ledTo.Domain()}))
		})

		It("skips a link stored unchanged", func() {
			plan, err := planImport(planInput([]export.Record{tl, stepdown, pager, ledTo}, storedFrom(tl, stepdown, pager, ledTo)))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.result.Links).To(Equal(importCounts{Identical: 1}))
			Expect(plan.write).To(Equal(store.ImportRecords{}))
		})

		It("names the timeline of the event a new link leaves as one it writes to", func() {
			plan, err := planImport(planInput([]export.Record{ledTo}, storedFrom(tl, stepdown, pager)))

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.write.Links).To(HaveLen(1))
			Expect(plan.timelineNames).To(Equal([]string{"inc-4821"}))
		})
	})

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
		links: map[string]timeline.Link{}, linksByJoin: map[string]timeline.Link{},
	}
	for _, r := range records {
		switch r := r.(type) {
		case export.Link:
			stored.links[r.ID] = r.Domain()
			stored.linksByJoin[linkJoin(r.FromEventID, r.ToEventID, r.Kind)] = r.Domain()
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
