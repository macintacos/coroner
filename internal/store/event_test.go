package store_test

import (
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Events", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	quarterPast := time.Date(2026, 9, 12, 14, 15, 0, 0, time.UTC)
	incident := timeline.Timeline{ID: uuid(1), Name: "inc-465", Refs: []string{}, CreatedAt: created, UpdatedAt: created}

	newEvent := func(n int, at time.Time) timeline.Event {
		return timeline.Event{
			ID:         uuid(n),
			TimelineID: incident.ID,
			OccurredAt: at,
			Precision:  timeline.PrecisionMinute,
			Tags:       []string{},
			Text:       "Pager fires for db-2",
			CreatedAt:  created,
			UpdatedAt:  created,
		}
	}
	seqs := func(events []timeline.Event) []int64 {
		out := make([]int64, 0, len(events))
		for _, e := range events {
			out = append(out, e.Seq)
		}
		return out
	}

	var s *store.Store

	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
	})

	It("reads an added event back by id, by seq and in its timeline", func(ctx SpecContext) {
		pager := newEvent(10, quarterPast)
		pager.OccurredOffset = new(-4 * 3600)
		pager.Approximate = true
		pager.OccurredRaw = new("around 10:15")
		pager.Source = new("pagerduty")
		pager.Tags = []string{"phase:detect", "team:db"}
		pager.URL = new("https://example.com/incidents/465")
		pager.Author = new("julian")

		added, err := s.AddEvent(ctx, pager, uuid(20))

		Expect(err).NotTo(HaveOccurred())
		pager.Seq = 1
		Expect(added).To(Equal(pager))
		Expect(s.Event(ctx, pager.ID)).To(Equal(pager))
		Expect(s.EventBySeq(ctx, incident.ID, 1)).To(Equal(pager))
		Expect(s.Events(ctx, incident.ID)).To(Equal([]timeline.Event{pager}))
	})

	It("reads back an interval event's ended at", func(ctx SpecContext) {
		failover := newEvent(10, quarterPast)
		failover.EndedAt = new(quarterPast.Add(20 * time.Minute))

		added, err := s.AddEvent(ctx, failover, uuid(20))
		Expect(err).NotTo(HaveOccurred())

		Expect(s.Event(ctx, failover.ID)).To(Equal(added))
		Expect(added.EndedAt).To(HaveValue(Equal(quarterPast.Add(20 * time.Minute))))
	})

	It("numbers a timeline's events from 1 and lists them by occurred at, then seq", func(ctx SpecContext) {
		for n, at := range []time.Time{quarterPast.Add(15 * time.Minute), quarterPast, quarterPast} {
			Expect(s.AddEvent(ctx, newEvent(10+n, at), uuid(20))).Error().NotTo(HaveOccurred())
		}

		events, err := s.Events(ctx, incident.ID)

		Expect(err).NotTo(HaveOccurred())
		Expect(seqs(events)).To(Equal([]int64{2, 3, 1}))
	})

	It("never gives the seq of a deleted event, even the latest, to another event", func(ctx SpecContext) {
		for n := range 3 {
			Expect(s.AddEvent(ctx, newEvent(10+n, quarterPast), uuid(20))).Error().NotTo(HaveOccurred())
		}
		Expect(s.DeleteEvents(ctx, uuid(12))).To(Succeed())

		added, err := s.AddEvent(ctx, newEvent(13, quarterPast), uuid(20))

		Expect(err).NotTo(HaveOccurred())
		Expect(added.Seq).To(Equal(int64(4)))
		Expect(s.EventBySeq(ctx, incident.ID, 3)).Error().To(MatchError(store.ErrNotFound))
	})

	It("finds events across timelines by id prefix, in id order and up to a limit", func(ctx SpecContext) {
		other := timeline.Timeline{ID: uuid(2), Name: "inc-466", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
		Expect(s.CreateTimeline(ctx, other)).To(Succeed())
		for _, e := range []timeline.Event{newEvent(11, quarterPast), newEvent(10, quarterPast), newEvent(20, quarterPast)} {
			Expect(s.AddEvent(ctx, e, uuid(30))).Error().NotTo(HaveOccurred())
		}
		elsewhere := newEvent(12, quarterPast)
		elsewhere.TimelineID = other.ID
		Expect(s.AddEvent(ctx, elsewhere, uuid(30))).Error().NotTo(HaveOccurred())
		ids := func(prefix string, limit int) []string {
			GinkgoHelper()
			events, err := s.EventsByIDPrefix(ctx, prefix, limit)
			Expect(err).NotTo(HaveOccurred())
			out := []string{}
			for _, e := range events {
				out = append(out, e.ID)
			}
			return out
		}

		Expect(ids("0199a1b2-0000-7000-8000-00000000001", 10)).To(Equal([]string{uuid(10), uuid(11), uuid(12)}))
		Expect(ids("0199a1b2-0000-7000-8000-00000000001", 2)).To(Equal([]string{uuid(10), uuid(11)}))
		Expect(ids(uuid(20), 10)).To(Equal([]string{uuid(20)}))
		Expect(ids("0199a1b2-0000-7000-8000-00000000003", 10)).To(BeEmpty())
	})

	It("deletes events with their annotations", func(ctx SpecContext) {
		for n := range 2 {
			Expect(s.AddEvent(ctx, newEvent(10+n, quarterPast), uuid(30))).Error().NotTo(HaveOccurred())
		}
		eventID := uuid(10)
		Expect(s.AddAnnotation(ctx, timeline.Annotation{
			ID: uuid(40), TimelineID: incident.ID, EventID: &eventID, Kind: timeline.AnnotationEvidence,
			Body: "replSetStepDown", CreatedAt: created, UpdatedAt: created,
		}, uuid(30))).To(Succeed())

		Expect(s.DeleteEvents(ctx, uuid(10), uuid(11))).To(Succeed())

		Expect(s.Events(ctx, incident.ID)).To(BeEmpty())
		Expect(s.Annotations(ctx, incident.ID)).To(BeEmpty())
	})

	It("deletes none of the events when one does not exist", func(ctx SpecContext) {
		Expect(s.AddEvent(ctx, newEvent(10, quarterPast), uuid(30))).Error().NotTo(HaveOccurred())

		Expect(s.DeleteEvents(ctx, uuid(10), uuid(99))).To(MatchError(store.ErrNotFound))

		Expect(s.Events(ctx, incident.ID)).To(HaveLen(1))
	})

	It("creates a source on first use and reuses it after", func(ctx SpecContext) {
		for n := range 2 {
			e := newEvent(10+n, quarterPast)
			e.Source = new("slack")
			Expect(s.AddEvent(ctx, e, uuid(20+n))).Error().NotTo(HaveOccurred())
		}

		Expect(s.Sources(ctx)).To(Equal([]timeline.Source{{ID: uuid(20), Name: "slack", CreatedAt: created}}))
	})

	It("updates an event, replacing its tags and source", func(ctx SpecContext) {
		pager := newEvent(10, quarterPast)
		pager.Source = new("slack")
		pager.Tags = []string{"phase:detect", "team:db"}
		pager, err := s.AddEvent(ctx, pager, uuid(20))
		Expect(err).NotTo(HaveOccurred())

		pager.Text = "Pager fires for db-2 and db-3"
		pager.EndedAt = new(quarterPast.Add(time.Hour))
		pager.Source = new("pagerduty")
		pager.Tags = []string{"phase:respond", "team:db"}
		pager.UpdatedAt = created.Add(time.Hour)
		Expect(s.UpdateEvent(ctx, pager, uuid(21))).To(Succeed())

		Expect(s.Event(ctx, pager.ID)).To(Equal(pager))
	})

	It("writes nothing, not even a seq, when part of an add fails", func(ctx SpecContext) {
		bad := newEvent(10, quarterPast)
		bad.Tags = []string{"Phase Detect"}

		Expect(s.AddEvent(ctx, bad, uuid(20))).Error().To(HaveOccurred())

		Expect(s.Events(ctx, incident.ID)).To(BeEmpty())
		Expect(s.AddEvent(ctx, newEvent(11, quarterPast), uuid(20))).To(HaveField("Seq", int64(1)))
	})

	DescribeTable("reports an event or timeline that does not exist with ErrNotFound",
		func(ctx SpecContext, call func(*store.Store, SpecContext) error) {
			Expect(call(s, ctx)).To(MatchError(store.ErrNotFound))
		},
		Entry("AddEvent to an unknown timeline", func(s *store.Store, ctx SpecContext) error {
			e := newEvent(10, quarterPast)
			e.TimelineID = uuid(9)
			_, err := s.AddEvent(ctx, e, uuid(20))
			return err
		}),
		Entry("Event", func(s *store.Store, ctx SpecContext) error {
			_, err := s.Event(ctx, uuid(9))
			return err
		}),
		Entry("EventBySeq", func(s *store.Store, ctx SpecContext) error {
			_, err := s.EventBySeq(ctx, incident.ID, 9)
			return err
		}),
		Entry("UpdateEvent", func(s *store.Store, ctx SpecContext) error {
			return s.UpdateEvent(ctx, newEvent(9, quarterPast), uuid(20))
		}),
		Entry("DeleteEvents", func(s *store.Store, ctx SpecContext) error {
			return s.DeleteEvents(ctx, uuid(9))
		}),
	)

	It("gives writers on their own stores, adding to one timeline at once, every seq exactly once",
		func(ctx SpecContext) {
			const writers = 16
			path := filepath.Join(GinkgoT().TempDir(), "shared.db")
			stores := make([]*store.Store, writers)
			for n := range stores {
				opened, err := store.Open(ctx, path)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(opened.Close)
				stores[n] = opened
			}
			Expect(stores[0].CreateTimeline(ctx, incident)).To(Succeed())

			added := make([]int64, writers)
			var wg sync.WaitGroup
			for n, writer := range stores {
				wg.Go(func() {
					defer GinkgoRecover()
					e, err := writer.AddEvent(ctx, newEvent(100+n, quarterPast), uuid(200+n))
					Expect(err).NotTo(HaveOccurred())
					added[n] = e.Seq
				})
			}
			wg.Wait()

			Expect(added).To(ConsistOf(
				int64(1), int64(2), int64(3), int64(4), int64(5), int64(6), int64(7), int64(8),
				int64(9), int64(10), int64(11), int64(12), int64(13), int64(14), int64(15), int64(16),
			))
		})
})
