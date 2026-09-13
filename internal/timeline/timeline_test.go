package timeline_test

import (
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

func created() time.Time { return time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC) }

// quarterPast is 10:15 EDT, a minute-precision instant.
func quarterPast() time.Time { return time.Date(2026, 9, 12, 14, 15, 0, 0, time.UTC) }

func validEvent() timeline.Event {
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

func validAnnotation() timeline.Annotation {
	return timeline.Annotation{
		ID:         "0199a1b2-0000-7000-8000-000000000002",
		TimelineID: "0199a1b2-0000-7000-8000-000000000000",
		Kind:       timeline.AnnotationEvidence,
		Body:       "2026-09-12T14:15:03.180+00:00 mongod: election timeout",
		CreatedAt:  created(),
		UpdatedAt:  created(),
	}
}

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
		Expect(t.Validate()).NotTo(Succeed())
	})

	It("rejects a blank ref", func() {
		t := valid()
		t.Refs = append(t.Refs, " ")
		Expect(t.Validate()).NotTo(Succeed())
	})
})

var _ = Describe("Event.Validate", func() {
	It("accepts a well-formed event", func() {
		Expect(validEvent().Validate()).To(Succeed())
	})

	DescribeTable("rejects",
		func(mutate func(*timeline.Event)) {
			e := validEvent()
			mutate(&e)
			Expect(e.Validate()).NotTo(Succeed())
		},
		Entry("empty text", func(e *timeline.Event) { e.Text = "" }),
		Entry("whitespace-only text", func(e *timeline.Event) { e.Text = " \n\t" }),
		Entry("an unknown precision", func(e *timeline.Event) { e.Precision = "fortnight" }),
		Entry("an instant inside its precision window rather than at its start",
			func(e *timeline.Event) { e.OccurredAt = quarterPast().Add(3 * time.Second) }),
		Entry("an hour window measured in UTC rather than the observed +05:30",
			func(e *timeline.Event) {
				e.Precision = timeline.PrecisionHour
				e.OccurredOffset = new(19800)
				e.OccurredAt = time.Date(2026, 9, 12, 5, 0, 0, 0, time.UTC)
			}),
		Entry("an ended at before occurred at",
			func(e *timeline.Event) { e.EndedAt = new(quarterPast().Add(-time.Minute)) }),
		Entry("a year the canonical instant cannot render",
			func(e *timeline.Event) { e.OccurredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }),
		Entry("a source outside the grammar", func(e *timeline.Event) { e.Source = new("Slack") }),
		Entry("a tag outside the grammar", func(e *timeline.Event) { e.Tags = []string{"Phase Detect"} }),
	)

	It("accepts an hour window measured in the observed +05:30", func() {
		e := validEvent()
		e.Precision = timeline.PrecisionHour
		e.OccurredOffset = new(19800)
		e.OccurredAt = time.Date(2026, 9, 12, 4, 30, 0, 0, time.UTC)
		Expect(e.Validate()).To(Succeed())
	})

	It("accepts an interval that ends when it starts", func() {
		e := validEvent()
		e.EndedAt = new(quarterPast())
		Expect(e.Validate()).To(Succeed())
	})

	It("reports every problem at once", func() {
		e := validEvent()
		e.Text = ""
		e.Tags = []string{"Phase Detect"}
		Expect(e.Validate()).To(MatchError(And(ContainSubstring("text"), ContainSubstring("Phase Detect"))))
	})
})

var _ = Describe("Annotation.Validate", func() {
	It("accepts well-formed evidence on the timeline itself", func() {
		Expect(validAnnotation().Validate()).To(Succeed())
	})

	It("accepts a comment on an event", func() {
		a := validAnnotation()
		a.Kind = timeline.AnnotationComment
		a.EventID = new("0199a1b2-0000-7000-8000-000000000001")
		Expect(a.Validate()).To(Succeed())
	})

	DescribeTable("rejects",
		func(mutate func(*timeline.Annotation)) {
			a := validAnnotation()
			mutate(&a)
			Expect(a.Validate()).NotTo(Succeed())
		},
		Entry("empty body", func(a *timeline.Annotation) { a.Body = "" }),
		Entry("whitespace-only body", func(a *timeline.Annotation) { a.Body = "  " }),
		Entry("an unknown kind", func(a *timeline.Annotation) { a.Kind = "note" }),
		Entry("a source outside the grammar", func(a *timeline.Annotation) { a.Source = new("db 2") }),
	)
})

var _ = Describe("Source.Validate", func() {
	It("accepts a name in the grammar and rejects one outside it", func() {
		Expect(timeline.Source{ID: "0199a1b2-0000-7000-8000-000000000003", Name: "db-2/mongod", CreatedAt: created()}.Validate()).To(Succeed())
		Expect(timeline.Source{ID: "0199a1b2-0000-7000-8000-000000000003", Name: "db-2 mongod", CreatedAt: created()}.Validate()).NotTo(Succeed())
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

	It("puts a coarse 10:15 before every 10:15:xx event, even one added earlier", func() {
		logLine := event(1, quarterPast().Add(3*time.Second+180*time.Millisecond))
		recalled := event(2, quarterPast())
		events := []timeline.Event{logLine, recalled}
		slices.SortFunc(events, timeline.CompareEvents)
		Expect(seqs(events)).To(Equal([]int64{2, 1}))
	})
})
