package store_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("TagUsages", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	at := func(minute int) time.Time { return time.Date(2026, 9, 12, 14, minute, 0, 0, time.UTC) }
	incident := timeline.Timeline{ID: uuid(1), Name: "inc-465", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
	other := timeline.Timeline{ID: uuid(2), Name: "inc-466", Refs: []string{}, CreatedAt: created, UpdatedAt: created}

	newEvent := func(n int, timelineID string, occurred time.Time, tags ...string) timeline.Event {
		return timeline.Event{
			ID:         uuid(n),
			TimelineID: timelineID,
			OccurredAt: occurred,
			Precision:  timeline.PrecisionMinute,
			Tags:       tags,
			Text:       "Pager fires for db-2",
			CreatedAt:  created,
			UpdatedAt:  created,
		}
	}

	var s *store.Store

	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		Expect(s.CreateTimeline(ctx, other)).To(Succeed())
	})

	It("counts each tag's events across timelines and spans their occurred at, by name", func(ctx SpecContext) {
		Expect(s.AddEvent(ctx, newEvent(10, incident.ID, at(20), "phase:detect", "election"), "")).Error().NotTo(HaveOccurred())
		Expect(s.AddEvent(ctx, newEvent(11, other.ID, at(5), "phase:detect"), "")).Error().NotTo(HaveOccurred())
		Expect(s.AddEvent(ctx, newEvent(12, incident.ID, at(10)), "")).Error().NotTo(HaveOccurred())

		Expect(s.TagUsages(ctx)).To(Equal([]store.TagUsage{
			{Name: "election", EventCount: 1, FirstOccurredAt: at(20), LastOccurredAt: at(20)},
			{Name: "phase:detect", EventCount: 2, FirstOccurredAt: at(5), LastOccurredAt: at(20)},
		}))
	})
})
