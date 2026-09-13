package store_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Annotations", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	incident := timeline.Timeline{ID: uuid(1), Name: "inc-465", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
	pager := timeline.Event{
		ID:         uuid(2),
		TimelineID: incident.ID,
		OccurredAt: time.Date(2026, 9, 12, 14, 15, 0, 0, time.UTC),
		Precision:  timeline.PrecisionMinute,
		Tags:       []string{},
		Text:       "Pager fires for db-2",
		CreatedAt:  created,
		UpdatedAt:  created,
	}

	var (
		s                   *store.Store
		evidence, rootCause timeline.Annotation
	)

	BeforeEach(func(ctx SpecContext) {
		s = openStore(ctx)
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		Expect(s.AddEvent(ctx, pager, uuid(9))).Error().NotTo(HaveOccurred())

		evidence = timeline.Annotation{
			ID:         uuid(10),
			TimelineID: incident.ID,
			EventID:    new(pager.ID),
			Kind:       timeline.AnnotationEvidence,
			Body:       "mongod: election timeout after 10000ms",
			Author:     new("julian"),
			Source:     new("db-2/mongod"),
			URL:        new("https://example.com/logs/db-2"),
			CreatedAt:  created,
			UpdatedAt:  created,
		}
		rootCause = timeline.Annotation{
			ID:         uuid(11),
			TimelineID: incident.ID,
			Kind:       timeline.AnnotationComment,
			Body:       "Election timeout, not a network partition",
			CreatedAt:  created.Add(time.Minute),
			UpdatedAt:  created.Add(time.Minute),
		}
	})

	It("reads an event's annotation back by id, in its timeline and on its event", func(ctx SpecContext) {
		Expect(s.AddAnnotation(ctx, evidence, uuid(20))).To(Succeed())

		Expect(s.Annotation(ctx, evidence.ID)).To(Equal(evidence))
		Expect(s.Annotations(ctx, incident.ID)).To(Equal([]timeline.Annotation{evidence}))
		Expect(s.EventAnnotations(ctx, pager.ID)).To(Equal([]timeline.Annotation{evidence}))
	})

	It("lists a timeline-level annotation in its timeline but on no event", func(ctx SpecContext) {
		Expect(s.AddAnnotation(ctx, evidence, uuid(20))).To(Succeed())
		Expect(s.AddAnnotation(ctx, rootCause, uuid(21))).To(Succeed())

		Expect(s.Annotations(ctx, incident.ID)).To(Equal([]timeline.Annotation{evidence, rootCause}))
		Expect(s.EventAnnotations(ctx, pager.ID)).To(Equal([]timeline.Annotation{evidence}))
	})

	It("creates the annotation's source on first use", func(ctx SpecContext) {
		Expect(s.AddAnnotation(ctx, evidence, uuid(20))).To(Succeed())

		Expect(s.Sources(ctx)).To(Equal([]timeline.Source{{ID: uuid(20), Name: "db-2/mongod", CreatedAt: created}}))
	})

	It("updates an annotation's kind, body, author, source and URL", func(ctx SpecContext) {
		Expect(s.AddAnnotation(ctx, evidence, uuid(20))).To(Succeed())
		evidence.Kind = timeline.AnnotationComment
		evidence.Body = "The timeout came from a GC pause"
		evidence.Author = nil
		evidence.Source = new("slack")
		evidence.URL = nil
		evidence.UpdatedAt = created.Add(time.Hour)

		Expect(s.UpdateAnnotation(ctx, evidence, uuid(21))).To(Succeed())

		Expect(s.Annotation(ctx, evidence.ID)).To(Equal(evidence))
	})

	It("deletes an annotation", func(ctx SpecContext) {
		Expect(s.AddAnnotation(ctx, evidence, uuid(20))).To(Succeed())

		Expect(s.DeleteAnnotation(ctx, evidence.ID)).To(Succeed())

		Expect(s.Annotation(ctx, evidence.ID)).Error().To(MatchError(store.ErrNotFound))
	})

	DescribeTable("reports an annotation that does not exist with ErrNotFound",
		func(ctx SpecContext, call func(*store.Store, SpecContext) error) {
			Expect(call(s, ctx)).To(MatchError(store.ErrNotFound))
		},
		Entry("UpdateAnnotation", func(s *store.Store, ctx SpecContext) error {
			return s.UpdateAnnotation(ctx, rootCause, uuid(20))
		}),
		Entry("DeleteAnnotation", func(s *store.Store, ctx SpecContext) error {
			return s.DeleteAnnotation(ctx, uuid(99))
		}),
	)
})
