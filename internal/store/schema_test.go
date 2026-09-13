package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

func id(n int) string { return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n) }

var _ = Describe("Schema", func() {
	now := timeline.FormatInstant(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	quarterPast := time.Date(2026, 9, 12, 10, 15, 0, 0, time.UTC)

	var (
		queries         *sqlcgen.Queries
		incident, other sqlcgen.Timeline
		slack           sqlcgen.Source
		pager           sqlcgen.Event
	)

	eventParams := func(n int, seq int64) sqlcgen.CreateEventParams {
		return sqlcgen.CreateEventParams{
			ID:                id(n),
			TimelineID:        incident.ID,
			Seq:               seq,
			OccurredAt:        timeline.FormatInstant(quarterPast),
			OccurredPrecision: string(timeline.PrecisionMinute),
			SourceID:          &slack.ID,
			Text:              "Pager fires for db-2",
			CreatedAt:         now,
			UpdatedAt:         now,
		}
	}
	annotationParams := func(n int, timelineID string, eventID *string) sqlcgen.CreateAnnotationParams {
		return sqlcgen.CreateAnnotationParams{
			ID:         id(n),
			TimelineID: timelineID,
			EventID:    eventID,
			Kind:       string(timeline.AnnotationComment),
			Body:       "Election timeout, not a network partition",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}

	BeforeEach(func(ctx SpecContext) {
		conn, err := store.Open(ctx, filepath.Join(GinkgoT().TempDir(), "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(conn.Close)
		queries = sqlcgen.New(conn)

		incident, err = queries.CreateTimeline(ctx, sqlcgen.CreateTimelineParams{
			ID: id(1), Name: "inc-465", CreatedAt: now, UpdatedAt: now,
		})
		Expect(err).NotTo(HaveOccurred())
		other, err = queries.CreateTimeline(ctx, sqlcgen.CreateTimelineParams{
			ID: id(2), Name: "inc-466", CreatedAt: now, UpdatedAt: now,
		})
		Expect(err).NotTo(HaveOccurred())
		slack, err = queries.CreateSource(ctx, sqlcgen.CreateSourceParams{ID: id(3), Name: "slack", CreatedAt: now})
		Expect(err).NotTo(HaveOccurred())
		pager, err = queries.CreateEvent(ctx, eventParams(4, 1))
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("rejects",
		func(ctx SpecContext, constraint string, write func(context.Context) error) {
			Expect(write(ctx)).To(MatchError(ContainSubstring(constraint + " constraint failed")))
		},
		Entry("an occurred at with one fractional digit", "CHECK", func(ctx context.Context) error {
			params := eventParams(10, 2)
			params.OccurredAt = "2026-09-12T10:15:00.1Z"
			_, err := queries.CreateEvent(ctx, params)
			return err
		}),
		Entry("an ended at before occurred at", "CHECK", func(ctx context.Context) error {
			params := eventParams(10, 2)
			params.EndedAt = new(timeline.FormatInstant(quarterPast.Add(-time.Minute)))
			_, err := queries.CreateEvent(ctx, params)
			return err
		}),
		Entry("a tag outside the grammar", "CHECK", func(ctx context.Context) error {
			return queries.AddEventTag(ctx, sqlcgen.AddEventTagParams{EventID: pager.ID, Tag: "Phase Detect"})
		}),
		Entry("an uppercase timeline name", "CHECK", func(ctx context.Context) error {
			_, err := queries.CreateTimeline(ctx, sqlcgen.CreateTimelineParams{
				ID: id(10), Name: "Inc-467", CreatedAt: now, UpdatedAt: now,
			})
			return err
		}),
		Entry("an annotation on an event in another timeline", "FOREIGN KEY", func(ctx context.Context) error {
			_, err := queries.CreateAnnotation(ctx, annotationParams(10, other.ID, &pager.ID))
			return err
		}),
		Entry("deleting a source an event still uses", "FOREIGN KEY", func(ctx context.Context) error {
			return queries.DeleteSource(ctx, slack.ID)
		}),
	)

	It("accepts an annotation on an event in its own timeline", func(ctx SpecContext) {
		Expect(queries.CreateAnnotation(ctx, annotationParams(10, incident.ID, &pager.ID))).
			To(HaveField("EventID", HaveValue(Equal(pager.ID))))
	})

	It("accepts an annotation on the timeline itself", func(ctx SpecContext) {
		Expect(queries.CreateAnnotation(ctx, annotationParams(10, other.ID, nil))).
			To(HaveField("EventID", BeNil()))
	})

	It("clears the current timeline when that timeline is deleted", func(ctx SpecContext) {
		Expect(queries.SetCurrentTimeline(ctx, incident.ID)).To(Succeed())
		Expect(queries.DeleteTimeline(ctx, incident.ID)).To(Succeed())

		_, err := queries.GetCurrentTimeline(ctx)
		Expect(err).To(MatchError(sql.ErrNoRows))
	})

	It("keeps the current timeline when another timeline is deleted", func(ctx SpecContext) {
		Expect(queries.SetCurrentTimeline(ctx, other.ID)).To(Succeed())
		Expect(queries.DeleteTimeline(ctx, incident.ID)).To(Succeed())

		Expect(queries.GetCurrentTimeline(ctx)).To(Equal(other.ID))
	})

	It("allocates each timeline's seqs from 1 without reuse", func(ctx SpecContext) {
		Expect(queries.AllocateSeq(ctx, other.ID)).To(Equal(int64(1)))
		Expect(queries.AllocateSeq(ctx, other.ID)).To(Equal(int64(2)))
	})

	It("upserts a source by name, keeping an existing source's id", func(ctx SpecContext) {
		Expect(queries.UpsertSource(ctx, sqlcgen.UpsertSourceParams{ID: id(10), Name: "slack", CreatedAt: now})).
			To(HaveField("ID", slack.ID))
		Expect(queries.UpsertSource(ctx, sqlcgen.UpsertSourceParams{ID: id(11), Name: "db-2/mongod", CreatedAt: now})).
			To(HaveField("ID", id(11)))
	})

	It("lists a timeline's refs in the order they were added", func(ctx SpecContext) {
		for _, ref := range []string{"https://example.com/browse/INC-465", "INC-465"} {
			Expect(queries.AddTimelineRef(ctx, sqlcgen.AddTimelineRefParams{TimelineID: incident.ID, Ref: ref})).To(Succeed())
		}

		Expect(queries.ListTimelineRefs(ctx, incident.ID)).
			To(Equal([]string{"https://example.com/browse/INC-465", "INC-465"}))
	})
})
