package timeline_test

import (
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

func created() time.Time { return time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC) }

func quarterPast() time.Time { return time.Date(2026, 9, 12, 10, 15, 0, 0, time.UTC) }

var _ = Describe("Timeline.Validate", func() {
	valid := func() timeline.Timeline {
		return timeline.Timeline{
			ID:        "0199a1b2-0000-7000-8000-000000000000",
			Name:      "inc-465",
			Refs:      []string{"INC-465", "https://example.com/browse/INC-465"},
			CreatedAt: created(),
			UpdatedAt: created(),
		}
	}

	It("accepts a well-formed timeline", func() {
		Expect(valid().Validate()).To(Succeed())
	})

	It("rejects a name outside the grammar", func() {
		t := valid()
		t.Name = "INC 465"
		Expect(t.Validate()).To(MatchError(HavePrefix("timeline name")))
	})

	It("rejects a blank ref", func() {
		t := valid()
		t.Refs = append(t.Refs, " ")
		Expect(t.Validate()).To(MatchError(HavePrefix("ref")))
	})
})

var _ = Describe("Event.Validate", func() {
	valid := func() timeline.Event {
		return timeline.Event{
			ID:         "0199a1b2-0000-7000-8000-000000000001",
			TimelineID: "0199a1b2-0000-7000-8000-000000000000",
			Seq:        1,
			OccurredAt: quarterPast(),
			Precision:  timeline.PrecisionMinute,
			Source:     new("slack/inc-465"),
			Tags:       []string{"phase:detect"},
			Text:       "Pager fires for db-2",
			CreatedAt:  created(),
			UpdatedAt:  created(),
		}
	}

	It("accepts a well-formed event", func() {
		Expect(valid().Validate()).To(Succeed())
	})

	DescribeTable("rejects",
		func(rule string, mutate func(*timeline.Event)) {
			e := valid()
			mutate(&e)
			Expect(e.Validate()).To(MatchError(HavePrefix(rule)))
		},
		Entry("empty text", "text", func(e *timeline.Event) { e.Text = "" }),
		Entry("whitespace-only text", "text", func(e *timeline.Event) { e.Text = " \n\t" }),
		Entry("an unknown precision", "precision", func(e *timeline.Event) { e.Precision = "fortnight" }),
		Entry("an instant inside its precision window rather than at its start", "occurred at",
			func(e *timeline.Event) { e.OccurredAt = quarterPast().Add(3 * time.Second) }),
		Entry("an hour window measured in UTC rather than the stated +05:30", "occurred at",
			func(e *timeline.Event) {
				e.Precision = timeline.PrecisionHour
				e.OccurredOffset = new(19800)
				e.OccurredAt = time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC)
			}),
		Entry("an ended at before occurred at", "ended at",
			func(e *timeline.Event) { e.EndedAt = new(quarterPast().Add(-time.Minute)) }),
		Entry("an occurred at year the canonical instant cannot render", "occurred at",
			func(e *timeline.Event) { e.OccurredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }),
		Entry("an ended at year the canonical instant cannot render", "ended at",
			func(e *timeline.Event) { e.EndedAt = new(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)) }),
		Entry("an occurred offset more than a day east of UTC", "occurred offset +24:30",
			func(e *timeline.Event) { e.OccurredOffset = new(88200) }),
		Entry("an occurred offset more than a day west of UTC", "occurred offset -24:59",
			func(e *timeline.Event) { e.OccurredOffset = new(-89940) }),
		Entry("a source outside the grammar", "source", func(e *timeline.Event) { e.Source = new("Slack") }),
		Entry("a tag outside the grammar", "tag", func(e *timeline.Event) { e.Tags = []string{"Phase Detect"} }),
	)

	It("accepts an hour window measured in the stated +05:30", func() {
		e := valid()
		e.Precision = timeline.PrecisionHour
		e.OccurredOffset = new(19800)
		e.OccurredAt = time.Date(2026, 9, 12, 4, 30, 0, 0, time.UTC)
		Expect(e.Validate()).To(Succeed())
	})

	DescribeTable("accepts an occurred offset of exactly a day",
		func(offset int) {
			e := valid()
			e.OccurredOffset = new(offset)
			e.OccurredAt = quarterPast()
			Expect(e.Validate()).To(Succeed())
		},
		Entry("east", 86400),
		Entry("west", -86400),
	)

	It("accepts an interval that ends when it starts", func() {
		e := valid()
		e.EndedAt = new(quarterPast())
		Expect(e.Validate()).To(Succeed())
	})

	It("reports every problem at once", func() {
		e := valid()
		e.Text = ""
		e.Tags = []string{"Phase Detect"}
		Expect(e.Validate()).To(MatchError(And(ContainSubstring("text"), ContainSubstring("Phase Detect"))))
	})

	It("reports an unknown precision and an out-of-range occurred at together", func() {
		e := valid()
		e.Precision = "fortnight"
		e.OccurredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		Expect(e.Validate()).To(MatchError(And(ContainSubstring("precision"), ContainSubstring("occurred at"))))
	})
})

var _ = Describe("Annotation.Validate", func() {
	valid := func() timeline.Annotation {
		return timeline.Annotation{
			ID:         "0199a1b2-0000-7000-8000-000000000002",
			TimelineID: "0199a1b2-0000-7000-8000-000000000000",
			Kind:       timeline.AnnotationEvidence,
			Body:       "2026-09-12T14:15:03.180+00:00 mongod: election timeout",
			CreatedAt:  created(),
			UpdatedAt:  created(),
		}
	}

	It("accepts well-formed evidence on the timeline itself", func() {
		Expect(valid().Validate()).To(Succeed())
	})

	It("accepts a comment on an event", func() {
		a := valid()
		a.Kind = timeline.AnnotationComment
		a.EventID = new("0199a1b2-0000-7000-8000-000000000001")
		Expect(a.Validate()).To(Succeed())
	})

	DescribeTable("rejects",
		func(rule string, mutate func(*timeline.Annotation)) {
			a := valid()
			mutate(&a)
			Expect(a.Validate()).To(MatchError(HavePrefix(rule)))
		},
		Entry("empty body", "body", func(a *timeline.Annotation) { a.Body = "" }),
		Entry("whitespace-only body", "body", func(a *timeline.Annotation) { a.Body = "  " }),
		Entry("an unknown kind", "annotation kind", func(a *timeline.Annotation) { a.Kind = "note" }),
		Entry("a source outside the grammar", "source", func(a *timeline.Annotation) { a.Source = new("db 2") }),
	)
})

var _ = Describe("Source.Validate", func() {
	It("accepts a name in the grammar", func() {
		Expect(timeline.Source{ID: "0199a1b2-0000-7000-8000-000000000003", Name: "db-2/mongod", CreatedAt: created()}.Validate()).To(Succeed())
	})

	It("rejects a name outside the grammar", func() {
		Expect(timeline.Source{ID: "0199a1b2-0000-7000-8000-000000000003", Name: "db-2 mongod", CreatedAt: created()}.Validate()).To(MatchError(HavePrefix("source")))
	})
})

var _ = Describe("CompareEvents", func() {
	event := func(seq int64, at time.Time) timeline.Event {
		return timeline.Event{Seq: seq, OccurredAt: at}
	}
	seqs := func(events []timeline.Event) []int64 {
		out := make([]int64, 0, len(events))
		for _, e := range events {
			out = append(out, e.Seq)
		}
		return out
	}

	It("orders by occurred at, then by seq", func() {
		events := []timeline.Event{
			event(1, quarterPast().Add(time.Hour)),
			event(4, quarterPast()),
			event(2, quarterPast()),
			event(3, quarterPast().Add(-time.Hour)),
		}
		slices.SortFunc(events, timeline.CompareEvents)
		Expect(seqs(events)).To(Equal([]int64{3, 2, 4, 1}))
	})

	It("orders events of different timelines at one instant by timeline id, then seq", func() {
		events := []timeline.Event{
			{TimelineID: "b", Seq: 1, OccurredAt: quarterPast()},
			{TimelineID: "a", Seq: 2, OccurredAt: quarterPast()},
			{TimelineID: "a", Seq: 1, OccurredAt: quarterPast()},
		}
		slices.SortFunc(events, timeline.CompareEvents)
		Expect(events).To(Equal([]timeline.Event{
			{TimelineID: "a", Seq: 1, OccurredAt: quarterPast()},
			{TimelineID: "a", Seq: 2, OccurredAt: quarterPast()},
			{TimelineID: "b", Seq: 1, OccurredAt: quarterPast()},
		}))
	})

	It("puts a coarse 10:15 before every 10:15:xx event, even one added earlier", func() {
		logLine := event(1, quarterPast().Add(3*time.Second+180*time.Millisecond))
		recalled := event(2, quarterPast())
		events := []timeline.Event{logLine, recalled}
		slices.SortFunc(events, timeline.CompareEvents)
		Expect(seqs(events)).To(Equal([]int64{2, 1}))
	})
})
