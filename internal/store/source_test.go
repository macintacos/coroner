package store_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Sources", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	at := func(minute int) time.Time { return time.Date(2026, 9, 12, 14, minute, 0, 0, time.UTC) }
	incident := timeline.Timeline{ID: uuid(1), Name: "inc-465", Refs: []string{}, CreatedAt: created, UpdatedAt: created}

	newEvent := func(n int, occurred time.Time, source string) timeline.Event {
		return timeline.Event{
			ID:         uuid(n),
			TimelineID: incident.ID,
			OccurredAt: occurred,
			Precision:  timeline.PrecisionMinute,
			Source:     new(source),
			Tags:       []string{},
			Text:       "Pager fires for db-2",
			CreatedAt:  created,
			UpdatedAt:  created,
		}
	}

	var s *store.Store

	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
	})

	Describe("SourceUsages", func() {
		It("returns an empty, non-nil slice when there are no sources", func(ctx SpecContext) {
			Expect(s.SourceUsages(ctx)).To(And(Not(BeNil()), BeEmpty()))
		})

		It("counts each source's events and annotations and spans its events' occurred at, by name", func(ctx SpecContext) {
			Expect(s.AddEvent(ctx, newEvent(10, at(20), "mongod"), uuid(20))).Error().NotTo(HaveOccurred())
			Expect(s.AddEvent(ctx, newEvent(11, at(5), "mongod"), uuid(21))).Error().NotTo(HaveOccurred())
			Expect(s.AddEvent(ctx, newEvent(12, at(10), "db-2/mongod"), uuid(22))).Error().NotTo(HaveOccurred())
			annotation := timeline.Annotation{
				ID: uuid(30), TimelineID: incident.ID, Kind: timeline.AnnotationEvidence, Body: "election timeout",
				Source: new("mongod"), CreatedAt: created, UpdatedAt: created,
			}
			Expect(s.AddAnnotation(ctx, annotation, uuid(23))).To(Succeed())

			Expect(s.SourceUsages(ctx)).To(Equal([]store.SourceUsage{
				{
					ID: uuid(22), Name: "db-2/mongod", CreatedAt: created,
					EventCount: 1, FirstOccurredAt: new(at(10)), LastOccurredAt: new(at(10)),
				},
				{
					ID: uuid(20), Name: "mongod", CreatedAt: created,
					EventCount: 2, AnnotationCount: 1, FirstOccurredAt: new(at(5)), LastOccurredAt: new(at(20)),
				},
			}))
		})

		It("keeps a source whose events were all deleted, with no counts and no occurred at", func(ctx SpecContext) {
			Expect(s.AddEvent(ctx, newEvent(10, at(20), "slack"), uuid(20))).Error().NotTo(HaveOccurred())
			Expect(s.DeleteEvents(ctx, uuid(10))).To(Succeed())

			Expect(s.SourceUsages(ctx)).To(Equal([]store.SourceUsage{
				{ID: uuid(20), Name: "slack", CreatedAt: created},
			}))
		})
	})

	Describe("RenameSource", func() {
		BeforeEach(func(ctx SpecContext) {
			Expect(s.AddEvent(ctx, newEvent(10, at(20), "mongo"), uuid(20))).Error().NotTo(HaveOccurred())
			Expect(s.AddEvent(ctx, newEvent(11, at(5), "db-2/mongod"), uuid(21))).Error().NotTo(HaveOccurred())
		})

		It("moves the source's events to the new name", func(ctx SpecContext) {
			Expect(s.RenameSource(ctx, uuid(20), "mongodb")).To(Succeed())

			renamed, err := s.ListEvents(ctx, store.EventFilter{TimelineIDs: []string{incident.ID}, Sources: []string{"mongodb"}})
			Expect(err).NotTo(HaveOccurred())
			Expect(renamed.Events).To(HaveExactElements(HaveField("ID", uuid(10))))
			Expect(s.ListEvents(ctx, store.EventFilter{TimelineIDs: []string{incident.ID}, Sources: []string{"mongo"}})).
				To(HaveField("Events", BeEmpty()))
			Expect(s.Event(ctx, uuid(10))).To(HaveField("Source", HaveValue(Equal("mongodb"))))
		})

		It("refuses a name another source has", func(ctx SpecContext) {
			Expect(s.RenameSource(ctx, uuid(20), "db-2/mongod")).To(MatchError(store.ErrConflict))
		})

		It("reports a source that does not exist", func(ctx SpecContext) {
			Expect(s.RenameSource(ctx, uuid(99), "mongodb")).To(MatchError(store.ErrNotFound))
		})
	})
})
