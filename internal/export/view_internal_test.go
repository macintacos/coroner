package export

import (
	"bytes"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

// consistent returns an export of timeline t1 whose every reference resolves: events e1 and e2,
// annotation a1 on e1 and a2 on the whole timeline, attachments f1 on e1 and f2 on a1, and
// link l1 from e1 to e2.
func consistent() Envelope {
	env := NewEnvelope("0.1.0", at("2026-09-12T21:40:05Z"))
	env.Timelines = []Timeline{{ID: "t1", Name: "checkout", Title: "Checkout spike", Refs: []string{}}}
	e1 := instant("2026-09-11T03:10:00Z", timeline.PrecisionSecond, "")
	e1.ID, e1.TimelineID = "e1", "t1"
	e2 := instant("2026-09-11T03:20:00Z", timeline.PrecisionSecond, "")
	e2.ID, e2.TimelineID = "e2", "t1"
	env.Events = []Event{e1, e2}
	env.Annotations = []Annotation{
		{ID: "a1", TimelineID: "t1", EventID: new("e1"), CreatedAt: Instant(at("2026-09-12T09:00:00Z"))},
		{ID: "a2", TimelineID: "t1", CreatedAt: Instant(at("2026-09-12T09:01:00Z"))},
	}
	env.Attachments = []Attachment{{ID: "f1", EventID: new("e1")}, {ID: "f2", AnnotationID: new("a1")}}
	env.Links = []Link{{ID: "l1", FromEventID: "e1", ToEventID: "e2"}}

	return env
}

// overlaid returns consistent's export with a second timeline, t2, in it.
func overlaid() Envelope {
	env := consistent()
	env.Timelines = append(env.Timelines, Timeline{ID: "t2", Name: "payments", Refs: []string{}})

	return env
}

func event(id, timelineID, occurredAt string, seq int64) Event {
	e := instant(occurredAt, timeline.PrecisionSecond, "")
	e.ID, e.TimelineID, e.Seq = id, timelineID, &seq

	return e
}

var _ = Describe("prepare", func() {
	DescribeTable("fails, naming the record, unless the export is consistent",
		func(env Envelope, inconsistent func(*Envelope), record, missing string) {
			inconsistent(&env)

			_, err := prepare(env)
			Expect(err).To(MatchError(And(ContainSubstring(record), ContainSubstring(missing))))
		},
		Entry("no timeline", consistent(), func(env *Envelope) { env.Timelines = nil }, "want a timeline", "got none"),
		Entry("an event's timeline missing, in one timeline", consistent(),
			func(env *Envelope) { env.Events[0].TimelineID = "gone" }, "event e1", "gone"),
		Entry("an event's timeline missing, in an overlay", overlaid(),
			func(env *Envelope) { env.Events[1].TimelineID = "gone" }, "event e2", "gone"),
		Entry("an annotation's timeline missing", consistent(),
			func(env *Envelope) { env.Annotations[1].TimelineID = "gone" }, "annotation a2", "gone"),
		Entry("an annotation's event missing", consistent(),
			func(env *Envelope) { env.Annotations[0].EventID = new("gone") }, "annotation a1", "gone"),
		Entry("an attachment's event missing", consistent(),
			func(env *Envelope) { env.Attachments[0].EventID = new("gone") }, "attachment f1", "gone"),
		Entry("an attachment's annotation missing", consistent(),
			func(env *Envelope) { env.Attachments[1].AnnotationID = new("gone") }, "attachment f2", "gone"),
		Entry("a link's leaving event missing", consistent(),
			func(env *Envelope) { env.Links[0].FromEventID = "gone" }, "link l1", "gone"),
		Entry("a link's reaching event missing", consistent(),
			func(env *Envelope) { env.Links[0].ToEventID = "gone" }, "link l1", "gone"),
	)

	It("orders events by occurred at, timeline id, seq and id, whatever the envelope's order", func() {
		env := overlaid()
		env.Annotations, env.Attachments, env.Links = nil, nil, nil
		env.Events = []Event{
			event("e5", "t1", "2026-09-11T03:20:00Z", 1),
			event("e4", "t1", "2026-09-11T03:10:00Z", 2),
			event("e3", "t1", "2026-09-11T03:10:00Z", 1),
			event("e2", "t2", "2026-09-11T03:10:00Z", 1),
			event("e1", "t1", "2026-09-11T03:10:00Z", 1),
		}

		v, err := prepare(env)
		Expect(err).NotTo(HaveOccurred())
		ids := []string{}
		for _, e := range v.Events {
			ids = append(ids, e.ID)
		}
		Expect(ids).To(Equal([]string{"e1", "e3", "e4", "e2", "e5"}))
	})

	It("splits annotations into those on a whole timeline and those on each event, each by created at then id", func() {
		env := consistent()
		later, earlier := Instant(at("2026-09-12T10:00:00Z")), Instant(at("2026-09-12T08:00:00Z"))
		env.Annotations = []Annotation{
			{ID: "w2", TimelineID: "t1", CreatedAt: later},
			{ID: "b", TimelineID: "t1", EventID: new("e1"), CreatedAt: later},
			{ID: "w1", TimelineID: "t1", CreatedAt: later},
			{ID: "a", TimelineID: "t1", EventID: new("e1"), CreatedAt: later},
			{ID: "c", TimelineID: "t1", EventID: new("e1"), CreatedAt: earlier},
			{ID: "d", TimelineID: "t1", EventID: new("e2"), CreatedAt: earlier},
		}
		env.Attachments = nil

		v, err := prepare(env)
		Expect(err).NotTo(HaveOccurred())
		idsOf := func(annotations []Annotation) []string {
			ids := []string{}
			for _, a := range annotations {
				ids = append(ids, a.ID)
			}
			return ids
		}
		Expect(idsOf(v.OnTimeline)).To(Equal([]string{"w1", "w2"}))
		Expect(idsOf(v.ByEvent["e1"])).To(Equal([]string{"c", "a", "b"}))
		Expect(idsOf(v.ByEvent["e2"])).To(Equal([]string{"d"}))
	})

	It("names an overlay's timelines and leaves one timeline unnamed", func() {
		one, err := prepare(consistent())
		Expect(err).NotTo(HaveOccurred())
		several, err := prepare(overlaid())
		Expect(err).NotTo(HaveOccurred())

		Expect(one.Overlay).To(BeFalse())
		Expect(one.Names).To(BeEmpty())
		Expect(several.Overlay).To(BeTrue())
		Expect(several.Names).To(Equal(map[string]string{"t1": "checkout", "t2": "payments"}))
	})

	It("returns one timeline's title and name, and an overlay's names joined by +", func() {
		one, err := prepare(consistent())
		Expect(err).NotTo(HaveOccurred())
		several, err := prepare(overlaid())
		Expect(err).NotTo(HaveOccurred())

		title, name := one.title()
		Expect([]string{title, name}).To(Equal([]string{"Checkout spike", "checkout"}))
		title, name = several.title()
		Expect([]string{title, name}).To(Equal([]string{"", "checkout + payments"}))
	})

	It("orders sources by first appearance, with no source last, and gives each a palette slot that wraps", func() {
		env := consistent()
		env.Annotations, env.Attachments, env.Links = nil, nil, nil
		env.Events = nil
		for i := range len(sourcePalette()) + 1 {
			e := event("s"+string(rune('a'+i)), "t1", "2026-09-11T03:00:00Z", int64(i))
			e.Source = new(string(rune('a' + i)))
			env.Events = append(env.Events, e)
		}
		env.Events = append(env.Events, event("none", "t1", "2026-09-11T02:00:00Z", 0))

		v, err := prepare(env)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Sources).To(HaveLen(len(sourcePalette()) + 2))
		Expect(v.Sources[0]).To(Equal("a"))
		Expect(v.Sources[len(v.Sources)-1]).To(BeEmpty())
		Expect(v.slot("b")).To(Equal(1))
		Expect(v.slot(v.Sources[len(sourcePalette())])).To(Equal(v.slot("a")))
	})

	DescribeTable("every writer fails, writing nothing, on an inconsistent export",
		func(write func(Envelope) (int, error)) {
			env := consistent()
			env.Annotations[0].EventID = new("gone")

			n, err := write(env)
			Expect(err).To(MatchError(ContainSubstring("annotation a1: its event gone")))
			Expect(n).To(BeZero())
		},
		Entry("Markdown", func(env Envelope) (int, error) {
			var b bytes.Buffer
			err := WriteMarkdown(&b, env, MarkdownOptions{})
			return b.Len(), err
		}),
		Entry("Mermaid", func(env Envelope) (int, error) {
			var b bytes.Buffer
			err := WriteMermaid(&b, env, time.UTC)
			return b.Len(), err
		}),
		Entry("HTML", func(env Envelope) (int, error) {
			var b bytes.Buffer
			err := WriteHTML(&b, env, time.UTC)
			return b.Len(), err
		}),
		Entry("SVG", func(env Envelope) (int, error) {
			var b bytes.Buffer
			err := WriteSVG(&b, env, time.UTC)
			return b.Len(), err
		}),
	)
})
