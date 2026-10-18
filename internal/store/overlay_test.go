package store_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Overlays", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	newTimeline := func(n int, name string) timeline.Timeline {
		return timeline.Timeline{ID: uuid(n), Name: name, Refs: []string{}, CreatedAt: created, UpdatedAt: created}
	}
	failover, latency, disk := newTimeline(1, "mongo-failover"), newTimeline(2, "checkout-latency"), newTimeline(3, "db-2-disk")
	checkout := timeline.Overlay{ID: uuid(10), Name: "checkout-incident", TimelineIDs: []string{latency.ID, failover.ID}, CreatedAt: created}
	storage := timeline.Overlay{ID: uuid(11), Name: "db-2", TimelineIDs: []string{disk.ID, failover.ID}, CreatedAt: created}

	var s *store.Store

	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		for _, t := range []timeline.Timeline{failover, latency, disk} {
			Expect(s.CreateTimeline(ctx, t)).To(Succeed())
		}
		Expect(s.CreateOverlay(ctx, checkout)).To(Succeed())
		Expect(s.CreateOverlay(ctx, storage)).To(Succeed())
	})

	It("reads an overlay back by name, its timelines in the order given", func(ctx SpecContext) {
		Expect(s.OverlayByName(ctx, checkout.Name)).To(Equal(checkout))
	})

	It("reports a name no overlay has", func(ctx SpecContext) {
		Expect(s.OverlayByName(ctx, "inc-465")).Error().To(MatchError(store.ErrNotFound))
	})

	It("refuses a name another overlay has", func(ctx SpecContext) {
		twin := timeline.Overlay{ID: uuid(12), Name: checkout.Name, TimelineIDs: []string{disk.ID}, CreatedAt: created}

		Expect(s.CreateOverlay(ctx, twin)).To(MatchError(store.ErrConflict))
	})

	It("lists the overlays a timeline is in, by name", func(ctx SpecContext) {
		Expect(s.TimelineOverlays(ctx, failover.ID)).To(Equal([]timeline.Overlay{checkout, storage}))
		Expect(s.TimelineOverlays(ctx, disk.ID)).To(Equal([]timeline.Overlay{storage}))
	})

	It("drops a deleted timeline from its overlays, keeping the others in order", func(ctx SpecContext) {
		Expect(s.DeleteTimeline(ctx, failover.ID)).To(Succeed())

		Expect(s.OverlayByName(ctx, checkout.Name)).To(HaveField("TimelineIDs", []string{latency.ID}))
		Expect(s.OverlayByName(ctx, storage.Name)).To(HaveField("TimelineIDs", []string{disk.ID}))
	})
})
