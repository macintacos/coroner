package store_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Timelines", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)

	var (
		s        *store.Store
		incident timeline.Timeline
	)

	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		incident = timeline.Timeline{
			ID:        uuid(1),
			Name:      "inc-465",
			Title:     "db-2 primary flapped",
			Refs:      []string{"https://example.com/browse/INC-465", "INC-465"},
			CreatedAt: created,
			UpdatedAt: created,
		}
	})

	It("reads a created timeline back by id and by name, with its refs in the order given", func(ctx SpecContext) {
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())

		Expect(s.Timeline(ctx, incident.ID)).To(Equal(incident))
		Expect(s.TimelineByName(ctx, "inc-465")).To(Equal(incident))
	})

	It("lists timelines by name", func(ctx SpecContext) {
		later := timeline.Timeline{ID: uuid(2), Name: "inc-470", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
		Expect(s.CreateTimeline(ctx, later)).To(Succeed())
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())

		Expect(s.Timelines(ctx)).To(Equal([]timeline.Timeline{incident, later}))
	})

	It("writes nothing when part of a create fails", func(ctx SpecContext) {
		incident.Refs = []string{"INC-465", " "}

		Expect(s.CreateTimeline(ctx, incident)).NotTo(Succeed())

		Expect(s.Timelines(ctx)).To(BeEmpty())
	})

	It("refuses a name another timeline has with ErrConflict", func(ctx SpecContext) {
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		incident.ID = uuid(2)

		Expect(s.CreateTimeline(ctx, incident)).To(MatchError(store.ErrConflict))
	})

	It("updates a timeline's name, title and updated at, replacing its refs in the order given", func(ctx SpecContext) {
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		incident.Name = "inc-465-db2"
		incident.Title = "db-2 election storm"
		incident.Refs = []string{"INC-465", "OPS-42"}
		incident.UpdatedAt = created.Add(time.Hour)

		Expect(s.UpdateTimeline(ctx, incident)).To(Succeed())

		Expect(s.Timeline(ctx, incident.ID)).To(Equal(incident))
	})

	It("deletes a timeline", func(ctx SpecContext) {
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())

		Expect(s.DeleteTimeline(ctx, incident.ID)).To(Succeed())

		Expect(s.Timeline(ctx, incident.ID)).Error().To(MatchError(store.ErrNotFound))
	})

	DescribeTable("reports a timeline that does not exist with ErrNotFound",
		func(ctx SpecContext, call func(*store.Store, SpecContext) error) {
			Expect(call(s, ctx)).To(MatchError(store.ErrNotFound))
		},
		Entry("Timeline", func(s *store.Store, ctx SpecContext) error {
			_, err := s.Timeline(ctx, uuid(9))
			return err
		}),
		Entry("TimelineByName", func(s *store.Store, ctx SpecContext) error {
			_, err := s.TimelineByName(ctx, "inc-999")
			return err
		}),
		Entry("UpdateTimeline", func(s *store.Store, ctx SpecContext) error {
			return s.UpdateTimeline(ctx, timeline.Timeline{ID: uuid(9), Name: "inc-999", CreatedAt: created, UpdatedAt: created})
		}),
		Entry("DeleteTimeline", func(s *store.Store, ctx SpecContext) error {
			return s.DeleteTimeline(ctx, uuid(9))
		}),
		Entry("SetCurrentTimeline", func(s *store.Store, ctx SpecContext) error {
			return s.SetCurrentTimeline(ctx, uuid(9))
		}),
	)

	Describe("the current timeline", func() {
		BeforeEach(func(ctx SpecContext) {
			Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		})

		It("is ErrNotFound until one is set", func(ctx SpecContext) {
			Expect(s.CurrentTimeline(ctx)).Error().To(MatchError(store.ErrNotFound))
		})

		It("is the timeline last set", func(ctx SpecContext) {
			Expect(s.SetCurrentTimeline(ctx, incident.ID)).To(Succeed())

			Expect(s.CurrentTimeline(ctx)).To(Equal(incident))
		})

		It("is unset by ClearCurrentTimeline", func(ctx SpecContext) {
			Expect(s.SetCurrentTimeline(ctx, incident.ID)).To(Succeed())

			Expect(s.ClearCurrentTimeline(ctx)).To(Succeed())

			Expect(s.CurrentTimeline(ctx)).Error().To(MatchError(store.ErrNotFound))
		})
	})
})
