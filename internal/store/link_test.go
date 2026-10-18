package store_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Links", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	failover := timeline.Timeline{ID: uuid(1), Name: "mongo-failover", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
	latency := timeline.Timeline{ID: uuid(2), Name: "checkout-latency", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
	newEvent := func(n int, t timeline.Timeline, text string) timeline.Event {
		return timeline.Event{
			ID: uuid(n), TimelineID: t.ID, OccurredAt: time.Date(2026, 9, 11, 3, n, 0, 0, time.UTC),
			Precision: timeline.PrecisionMinute, Tags: []string{}, Text: text, CreatedAt: created, UpdatedAt: created,
		}
	}
	stepDown := newEvent(10, failover, "Primary stepped down")
	pager := newEvent(11, failover, "Pager fires for db-2")
	p99 := newEvent(12, latency, "Checkout p99 above 2s")
	newLink := func(n int, from, to timeline.Event, kind timeline.LinkKind) timeline.Link {
		return timeline.Link{
			ID: uuid(n), FromEventID: from.ID, ToEventID: to.ID, Kind: kind, CreatedAt: created.Add(time.Duration(n) * time.Second),
		}
	}
	ledTo := newLink(20, stepDown, p99, timeline.LinkLedTo)
	ledTo.Author = new("julian")
	sameAs := newLink(21, pager, stepDown, timeline.LinkSameAs)

	var s *store.Store

	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		for _, t := range []timeline.Timeline{failover, latency} {
			Expect(s.CreateTimeline(ctx, t)).To(Succeed())
		}
		for _, e := range []timeline.Event{stepDown, pager, p99} {
			Expect(s.AddEvent(ctx, e, "")).Error().NotTo(HaveOccurred())
		}
		Expect(s.AddLink(ctx, ledTo)).To(Succeed())
		Expect(s.AddLink(ctx, sameAs)).To(Succeed())
	})

	It("reads an event's links in both directions, oldest first", func(ctx SpecContext) {
		Expect(s.EventLinks(ctx, stepDown.ID)).To(Equal([]timeline.Link{ledTo, sameAs}))
		Expect(s.EventLinks(ctx, p99.ID)).To(Equal([]timeline.Link{ledTo}))
	})

	It("finds a link by id, and by its events and kind", func(ctx SpecContext) {
		Expect(s.Link(ctx, ledTo.ID)).To(Equal(ledTo))
		Expect(s.LinkBetween(ctx, stepDown.ID, p99.ID, timeline.LinkLedTo)).To(Equal(ledTo))
		Expect(s.LinkBetween(ctx, p99.ID, stepDown.ID, timeline.LinkLedTo)).Error().To(MatchError(store.ErrNotFound))
	})

	It("refuses a second link of one kind from one event to another", func(ctx SpecContext) {
		Expect(s.AddLink(ctx, newLink(22, stepDown, p99, timeline.LinkLedTo))).To(MatchError(store.ErrConflict))
		Expect(s.AddLink(ctx, newLink(23, stepDown, p99, timeline.LinkSameAs))).To(Succeed())
	})

	It("lists a link under the timeline of the event it leaves", func(ctx SpecContext) {
		Expect(s.TimelineLinks(ctx, failover.ID)).To(Equal([]timeline.Link{ledTo, sameAs}))
		Expect(s.TimelineLinks(ctx, latency.ID)).To(BeEmpty())
	})

	It("deletes a link, and reports one that does not exist", func(ctx SpecContext) {
		Expect(s.DeleteLink(ctx, ledTo.ID)).To(Succeed())

		Expect(s.EventLinks(ctx, p99.ID)).To(BeEmpty())
		Expect(s.DeleteLink(ctx, ledTo.ID)).To(MatchError(store.ErrNotFound))
	})

	It("deletes an event's links, in both directions, with the event", func(ctx SpecContext) {
		Expect(s.DeleteEvents(ctx, stepDown.ID)).To(Succeed())

		Expect(s.EventLinks(ctx, p99.ID)).To(BeEmpty())
		Expect(s.EventLinks(ctx, pager.ID)).To(BeEmpty())
	})
})
