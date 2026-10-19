package timeline_test

import (
	"errors"
	"slices"
	"strings"
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

	It("rejects a title that is not UTF-8", func() {
		t := valid()
		t.Title = "caf\xe9"
		Expect(t.Validate()).To(MatchError("title: must be UTF-8"))
	})

	It("rejects a ref that is not UTF-8", func() {
		t := valid()
		t.Refs = append(t.Refs, "INC-\xff")
		Expect(t.Validate()).To(MatchError("ref 3: must be UTF-8"))
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
		Entry("text that is not UTF-8", "text: must be UTF-8", func(e *timeline.Event) { e.Text = "caf\xe9" }),
		Entry("an occurred raw that is not UTF-8", "occurred_raw: must be UTF-8",
			func(e *timeline.Event) { e.OccurredRaw = new("10:15\xff") }),
		Entry("a url that is not UTF-8", "url: must be UTF-8", func(e *timeline.Event) { e.URL = new("https://x/\xff") }),
		Entry("an author that is not UTF-8", "author: must be UTF-8", func(e *timeline.Event) { e.Author = new("Ren\xe9") }),
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
		Entry("a body that is not UTF-8", "body: must be UTF-8", func(a *timeline.Annotation) { a.Body = "caf\xe9" }),
		Entry("a url that is not UTF-8", "url: must be UTF-8", func(a *timeline.Annotation) { a.URL = new("https://x/\xff") }),
		Entry("an author that is not UTF-8", "author: must be UTF-8", func(a *timeline.Annotation) { a.Author = new("Ren\xe9") }),
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

// fieldRules returns each problem err reports as its field and rule, without its message.
func fieldRules(err error) []timeline.Problem {
	verr, ok := errors.AsType[*timeline.ValidationError](err)
	Expect(ok).To(BeTrue(), "want a *timeline.ValidationError, got %v", err)
	out := make([]timeline.Problem, 0, len(verr.Problems))
	for _, p := range verr.Problems {
		out = append(out, timeline.Problem{Field: p.Field, Rule: p.Rule})
	}
	return out
}

var _ = Describe("ValidationError", func() {
	event := func() timeline.Event {
		return timeline.Event{OccurredAt: quarterPast(), Precision: timeline.PrecisionMinute, Text: "Pager fires"}
	}

	It("reports each problem's field and rule, in the order Validate checks them", func() {
		e := event()
		e.Text = ""
		e.EndedAt = new(quarterPast().Add(-time.Minute))
		Expect(fieldRules(e.Validate())).To(Equal([]timeline.Problem{
			{Field: "text", Rule: timeline.RuleBlank},
			{Field: "ended_at", Rule: timeline.RuleEndedBeforeOccurred},
		}))
	})

	It("joins messages as errors.Join did", func() {
		e := event()
		e.Text = ""
		e.EndedAt = new(quarterPast().Add(-time.Minute))
		Expect(e.Validate()).To(MatchError("text: must not be blank\n" +
			"ended at 2026-09-12T10:14:00.000000000Z: must not be before occurred at 2026-09-12T10:15:00.000000000Z"))
	})

	DescribeTable("names the field and rule of",
		func(err error, want timeline.Problem) {
			Expect(fieldRules(err)).To(Equal([]timeline.Problem{want}))
		},
		Entry("a self link", timeline.Link{FromEventID: "a", ToEventID: "a", Kind: timeline.LinkLedTo}.Validate(),
			timeline.Problem{Field: "to_event_id", Rule: timeline.RuleSelfLink}),
		Entry("an unknown link kind", timeline.Link{FromEventID: "a", ToEventID: "b", Kind: "after"}.Validate(),
			timeline.Problem{Field: "kind", Rule: timeline.RuleUnknown}),
		Entry("an unknown precision", func() error { e := event(); e.Precision = "fortnight"; return e.Validate() }(),
			timeline.Problem{Field: "precision", Rule: timeline.RuleUnknown}),
		Entry("a blank ref", timeline.Timeline{Name: "inc-465", Refs: []string{" "}}.Validate(),
			timeline.Problem{Field: "refs", Rule: timeline.RuleBlank}),
		Entry("the source none", timeline.Source{Name: "none"}.Validate(),
			timeline.Problem{Field: "name", Rule: timeline.RuleReserved}),
		Entry("the source none on an event", func() error { e := event(); e.Source = new("none"); return e.Validate() }(),
			timeline.Problem{Field: "source", Rule: timeline.RuleReserved}),
		Entry("a bad tag", func() error { e := event(); e.Tags = []string{"Phase"}; return e.Validate() }(),
			timeline.Problem{Field: "tags", Rule: timeline.RuleGrammar}),
		Entry("an out-of-range offset", func() error { e := event(); e.OccurredOffset = new(88200); return e.Validate() }(),
			timeline.Problem{Field: "occurred_offset", Rule: timeline.RuleOffsetRange}),
		Entry("a negative size", timeline.Attachment{
			Owner: timeline.AttachmentOwner{EventID: new("a")}, SHA256: strings.Repeat("ab", 32), Size: -1,
		}.Validate(), timeline.Problem{Field: "size", Rule: timeline.RuleNegativeSize}),
		Entry("a bad owner", timeline.AttachmentOwner{}.Validate(),
			timeline.Problem{Field: "event_id", Rule: timeline.RuleOwner}),
	)

	DescribeTable("Valid reports enum membership",
		func(valid, want bool) { Expect(valid).To(Equal(want)) },
		Entry("a precision", timeline.PrecisionHour.Valid(), true),
		Entry("an unknown precision", timeline.Precision("fortnight").Valid(), false),
		Entry("an annotation kind", timeline.AnnotationComment.Valid(), true),
		Entry("an unknown annotation kind", timeline.AnnotationKind("note").Valid(), false),
		Entry("a link kind", timeline.LinkSameAs.Valid(), true),
		Entry("an unknown link kind", timeline.LinkKind("after").Valid(), false),
	)
})
