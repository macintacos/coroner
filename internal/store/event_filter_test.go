package store_test

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("buildEventQuery", func() {
	It("passes hostile input only as args, one per placeholder", func() {
		const hostile = "'); DROP TABLE event; --"
		since := time.Date(2026, 9, 12, 14, 0, 0, 0, time.UTC)
		f := store.EventFilter{
			Since:    &since,
			Until:    new(since.Add(time.Hour)),
			Sources:  []string{hostile, hostile + "2"},
			Tags:     []string{hostile, hostile + "3"},
			Contains: hostile,
			Order:    store.OrderDesc,
			Limit:    5,
		}

		query, args := store.BuildEventQuery(hostile, f)

		Expect(query).NotTo(ContainSubstring("DROP"))
		Expect(args).To(ContainElements(hostile, hostile+"2", hostile+"3"))
		Expect(strings.Count(query, "?")).To(Equal(len(args)))
	})
})

var _ = Describe("ListEvents", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	at := func(minute int) time.Time { return time.Date(2026, 9, 12, 14, minute, 0, 0, time.UTC) }
	incident := timeline.Timeline{ID: uuid(1), Name: "inc-465", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
	other := timeline.Timeline{ID: uuid(2), Name: "inc-466", Refs: []string{}, CreatedAt: created, UpdatedAt: created}

	newEvent := func(n int, timelineID string, occurred time.Time, text string) timeline.Event {
		return timeline.Event{
			ID:         uuid(n),
			TimelineID: timelineID,
			OccurredAt: occurred,
			Precision:  timeline.PrecisionMinute,
			Tags:       []string{},
			Text:       text,
			CreatedAt:  created,
			UpdatedAt:  created,
		}
	}
	seqs := func(events []timeline.Event) []int64 {
		out := []int64{}
		for _, e := range events {
			out = append(out, e.Seq)
		}
		return out
	}

	var s *store.Store

	// Seqs 1–4 in incident, plus one event in another timeline that no filter may return.
	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		Expect(s.CreateTimeline(ctx, other)).To(Succeed())

		pager := newEvent(10, incident.ID, at(0), "Pager fires for DB-2")
		pager.OccurredOffset = new(-4 * 3600)
		pager.Approximate = true
		pager.OccurredRaw = new("around 10:00")
		pager.EndedAt = new(at(20))
		pager.Source = new("pagerduty")
		pager.Tags = []string{"team:db", "phase:detect"}
		pager.URL = new("https://example.com/incidents/465")
		pager.Author = new("julian")
		pager.UpdatedAt = created.Add(time.Hour)
		writes := newEvent(11, incident.ID, at(15), "50% of writes fail")
		writes.Source = new("slack")
		writes.Tags = []string{"team:db"}
		rollback := newEvent(12, incident.ID, at(15), "a_b rollback")
		rollback.Tags = []string{"phase:detect"}
		dashboards := newEvent(13, incident.ID, at(30), "abc dashboards red")
		dashboards.Source = new("grafana")
		elsewhere := newEvent(14, other.ID, at(15), "Pager fires for DB-2")
		elsewhere.Source = new("slack")
		elsewhere.Tags = []string{"team:db"}

		for n, e := range []timeline.Event{pager, writes, rollback, dashboards, elsewhere} {
			Expect(s.AddEvent(ctx, e, uuid(20+n))).Error().NotTo(HaveOccurred())
		}
	})

	It("returns every event of the timeline, as Events reads them, for the zero filter", func(ctx SpecContext) {
		events, err := s.Events(ctx, incident.ID)
		Expect(err).NotTo(HaveOccurred())

		Expect(s.ListEvents(ctx, incident.ID, store.EventFilter{})).To(Equal(store.EventPage{Events: events, Total: 4}))
	})

	It("returns an empty page for a timeline with no events", func(ctx SpecContext) {
		Expect(s.ListEvents(ctx, uuid(9), store.EventFilter{})).To(Equal(store.EventPage{Events: []timeline.Event{}}))
	})

	DescribeTable("rejects a bound outside years 0000–9999",
		func(ctx SpecContext, f store.EventFilter) {
			Expect(s.ListEvents(ctx, incident.ID, f)).Error().To(HaveOccurred())
		},
		Entry("Since", store.EventFilter{Since: new(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))}),
		Entry("Until", store.EventFilter{Until: new(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))}),
	)

	DescribeTable("keeps the events the filter selects, in order, up to its limit",
		func(ctx SpecContext, f store.EventFilter, want []int64, total int, truncated bool) {
			page, err := s.ListEvents(ctx, incident.ID, f)

			Expect(err).NotTo(HaveOccurred())
			Expect(seqs(page.Events)).To(Equal(want))
			Expect(page.Total).To(Equal(total))
			Expect(page.Truncated).To(Equal(truncated))
		},
		Entry("includes an event exactly at Since", store.EventFilter{Since: new(at(15))}, []int64{2, 3, 4}, 3, false),
		Entry("excludes an event exactly at Until", store.EventFilter{Until: new(at(15))}, []int64{1}, 1, false),
		Entry("keeps events carrying every tag", store.EventFilter{Tags: []string{"phase:detect", "team:db"}}, []int64{1}, 1, false),
		Entry("keeps events from any source", store.EventFilter{Sources: []string{"pagerduty", "slack"}}, []int64{1, 2}, 2, false),
		Entry("matches % in Contains literally", store.EventFilter{Contains: "%"}, []int64{2}, 1, false),
		Entry("matches _ in Contains literally", store.EventFilter{Contains: "_"}, []int64{3}, 1, false),
		Entry("matches Contains ignoring ASCII case", store.EventFilter{Contains: "db-2"}, []int64{1}, 1, false),
		Entry("orders descending by occurred at, then seq", store.EventFilter{Order: store.OrderDesc}, []int64{4, 3, 2, 1}, 4, false),
		Entry("truncates to Limit, counting every match", store.EventFilter{Limit: 2}, []int64{1, 2}, 4, true),
		Entry("limits after filtering and ordering",
			store.EventFilter{Tags: []string{"team:db"}, Order: store.OrderDesc, Limit: 1}, []int64{2}, 2, true),
		Entry("does not truncate at a Limit equal to the matches", store.EventFilter{Limit: 4}, []int64{1, 2, 3, 4}, 4, false),
		Entry("returns nothing for an unknown source", store.EventFilter{Sources: []string{"absent"}}, []int64{}, 0, false),
	)
})
