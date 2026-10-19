package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/store/sqlcgen"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Schema", func() {
	now := timeline.FormatInstant(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	quarterPast := time.Date(2026, 9, 12, 10, 15, 0, 0, time.UTC)

	var (
		queries         *sqlcgen.Queries
		incident, other sqlcgen.Timeline
		slack           sqlcgen.Source
		pager           sqlcgen.Event
	)

	id := func(n int) string { return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n) }
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
	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	attachmentParams := func(n int, eventID, annotationID *string) sqlcgen.CreateAttachmentParams {
		return sqlcgen.CreateAttachmentParams{
			ID:           id(n),
			EventID:      eventID,
			AnnotationID: annotationID,
			Filename:     "grafana.png",
			Mime:         "image/png",
			Sha256:       emptySHA256,
			CreatedAt:    now,
		}
	}

	linkParams := func(n int, toEventID string) sqlcgen.CreateLinkParams {
		return sqlcgen.CreateLinkParams{
			ID: id(n), FromEventID: pager.ID, ToEventID: toEventID, Kind: string(timeline.LinkLedTo), CreatedAt: now,
		}
	}
	// secondEvent adds an event after pager in its timeline, for a link to reach.
	secondEvent := func(ctx context.Context) string {
		GinkgoHelper()
		event, err := queries.CreateEvent(ctx, eventParams(90, 2))
		Expect(err).NotTo(HaveOccurred())
		return event.ID
	}

	BeforeEach(func(ctx SpecContext) {
		s, err := store.Open(ctx, filepath.Join(GinkgoT().TempDir(), "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		queries = sqlcgen.New(s.DB())

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
		seq, err := queries.AllocateSeq(ctx, incident.ID)
		Expect(err).NotTo(HaveOccurred())
		pager, err = queries.CreateEvent(ctx, eventParams(4, seq))
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
		Entry("deleting a source only an annotation uses", "FOREIGN KEY", func(ctx context.Context) error {
			source, err := queries.CreateSource(ctx, sqlcgen.CreateSourceParams{ID: id(10), Name: "db-2/mongod", CreatedAt: now})
			Expect(err).NotTo(HaveOccurred())
			params := annotationParams(11, incident.ID, nil)
			params.SourceID = &source.ID
			_, err = queries.CreateAnnotation(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			return queries.DeleteSource(ctx, source.ID)
		}),
		Entry("a precision outside the list", "CHECK", func(ctx context.Context) error {
			params := eventParams(10, 2)
			params.OccurredPrecision = "fortnight"
			_, err := queries.CreateEvent(ctx, params)
			return err
		}),
		Entry("an occurred offset beyond a day", "CHECK", func(ctx context.Context) error {
			params := eventParams(10, 2)
			params.OccurredOffset = new(int64(86401))
			_, err := queries.CreateEvent(ctx, params)
			return err
		}),
		Entry("an approximate that is not 0 or 1", "CHECK", func(ctx context.Context) error {
			params := eventParams(10, 2)
			params.Approximate = 2
			_, err := queries.CreateEvent(ctx, params)
			return err
		}),
		Entry("a white-space-only event text", "CHECK", func(ctx context.Context) error {
			params := eventParams(10, 2)
			params.Text = "\t\n"
			_, err := queries.CreateEvent(ctx, params)
			return err
		}),
		Entry("an annotation kind outside the list", "CHECK", func(ctx context.Context) error {
			params := annotationParams(10, incident.ID, nil)
			params.Kind = "note"
			_, err := queries.CreateAnnotation(ctx, params)
			return err
		}),
		Entry("a white-space-only annotation body", "CHECK", func(ctx context.Context) error {
			params := annotationParams(10, incident.ID, nil)
			params.Body = "\u3000"
			_, err := queries.CreateAnnotation(ctx, params)
			return err
		}),
		Entry("a white-space-only ref", "CHECK", func(ctx context.Context) error {
			return queries.AddTimelineRef(ctx, sqlcgen.AddTimelineRefParams{TimelineID: incident.ID, Ref: "\n"})
		}),
		Entry("an attachment on both an event and an annotation", "CHECK", func(ctx context.Context) error {
			annotation, err := queries.CreateAnnotation(ctx, annotationParams(10, incident.ID, &pager.ID))
			Expect(err).NotTo(HaveOccurred())
			_, err = queries.CreateAttachment(ctx, attachmentParams(11, &pager.ID, &annotation.ID))
			return err
		}),
		Entry("an attachment on neither an event nor an annotation", "CHECK", func(ctx context.Context) error {
			_, err := queries.CreateAttachment(ctx, attachmentParams(10, nil, nil))
			return err
		}),
		Entry("an attachment sha256 one digit short", "CHECK", func(ctx context.Context) error {
			params := attachmentParams(10, &pager.ID, nil)
			params.Sha256 = emptySHA256[1:]
			_, err := queries.CreateAttachment(ctx, params)
			return err
		}),
		Entry("an uppercase attachment sha256", "CHECK", func(ctx context.Context) error {
			params := attachmentParams(10, &pager.ID, nil)
			params.Sha256 = strings.ToUpper(emptySHA256)
			_, err := queries.CreateAttachment(ctx, params)
			return err
		}),
		Entry("an attachment sha256 that is not hex", "CHECK", func(ctx context.Context) error {
			params := attachmentParams(10, &pager.ID, nil)
			params.Sha256 = "g" + emptySHA256[1:]
			_, err := queries.CreateAttachment(ctx, params)
			return err
		}),
		Entry("a negative attachment size", "CHECK", func(ctx context.Context) error {
			params := attachmentParams(10, &pager.ID, nil)
			params.Size = -1
			_, err := queries.CreateAttachment(ctx, params)
			return err
		}),
		Entry("an attachment created at without nine fractional digits", "CHECK", func(ctx context.Context) error {
			params := attachmentParams(10, &pager.ID, nil)
			params.CreatedAt = "2026-09-13T09:00:00Z"
			_, err := queries.CreateAttachment(ctx, params)
			return err
		}),
		Entry("an attachment on an event that does not exist", "FOREIGN KEY", func(ctx context.Context) error {
			_, err := queries.CreateAttachment(ctx, attachmentParams(10, new(id(99)), nil))
			return err
		}),
		Entry("a link kind outside the list", "CHECK", func(ctx context.Context) error {
			params := linkParams(10, secondEvent(ctx))
			params.Kind = "after"
			_, err := queries.CreateLink(ctx, params)
			return err
		}),
		Entry("a link from an event to itself", "CHECK", func(ctx context.Context) error {
			_, err := queries.CreateLink(ctx, linkParams(10, pager.ID))
			return err
		}),
		Entry("a second link of one kind from one event to another", "UNIQUE", func(ctx context.Context) error {
			to := secondEvent(ctx)
			Expect(queries.CreateLink(ctx, linkParams(10, to))).Error().NotTo(HaveOccurred())
			_, err := queries.CreateLink(ctx, linkParams(11, to))
			return err
		}),
		Entry("a link to an event that does not exist", "FOREIGN KEY", func(ctx context.Context) error {
			_, err := queries.CreateLink(ctx, linkParams(10, id(99)))
			return err
		}),
		Entry("a link created at without nine fractional digits", "CHECK", func(ctx context.Context) error {
			params := linkParams(10, secondEvent(ctx))
			params.CreatedAt = "2026-09-13T09:00:00Z"
			_, err := queries.CreateLink(ctx, params)
			return err
		}),
		Entry("an overlay on a timeline that does not exist", "FOREIGN KEY", func(ctx context.Context) error {
			_, err := queries.CreateOverlay(ctx, sqlcgen.CreateOverlayParams{ID: id(10), Name: "checkout-incident", CreatedAt: now})
			Expect(err).NotTo(HaveOccurred())
			return queries.AddOverlayTimeline(ctx, sqlcgen.AddOverlayTimelineParams{OverlayID: id(10), TimelineID: id(99), Position: 1})
		}),
	)

	const nulLed = " \u0000abc"
	DescribeTable("rejects NUL-led text in both the not-blank CHECK and validation",
		func(ctx SpecContext, write func(context.Context) error, validationErr error) {
			Expect(write(ctx)).To(MatchError(ContainSubstring("CHECK constraint failed")))
			Expect(validationErr).To(HaveOccurred())
		},
		Entry("event text", func(ctx context.Context) error {
			params := eventParams(10, 2)
			params.Text = nulLed
			_, err := queries.CreateEvent(ctx, params)
			return err
		}, timeline.ValidateText("text", nulLed)),
		Entry("annotation body", func(ctx context.Context) error {
			params := annotationParams(10, incident.ID, nil)
			params.Body = nulLed
			_, err := queries.CreateAnnotation(ctx, params)
			return err
		}, timeline.ValidateText("body", nulLed)),
		Entry("timeline ref", func(ctx context.Context) error {
			return queries.AddTimelineRef(ctx, sqlcgen.AddTimelineRefParams{TimelineID: incident.ID, Ref: nulLed})
		}, timeline.Timeline{Name: "inc-465", Refs: []string{nulLed}}.Validate()),
	)

	checkFailed := ContainSubstring("CHECK constraint failed")
	expectCheckAgrees := func(err, validationErr error) {
		GinkgoHelper()
		if validationErr != nil {
			Expect(err).To(MatchError(checkFailed))
			return
		}
		// The fixture already holds inc-465, so an accepted name may still fail UNIQUE.
		Expect(err).To(Or(Not(HaveOccurred()), MatchError(Not(checkFailed))))
	}
	grammarCorpus := []any{}
	for _, input := range []string{
		"a", "inc-465", "db.2_x", strings.Repeat("a", 64), strings.Repeat("a", 65), "", "-a", ".a", "Inc",
		"inc 465", "phase:detect", "phase:Detect", "db-2/mongod", "a]", "a*", "é", "none",
	} {
		grammarCorpus = append(grammarCorpus, Entry(fmt.Sprintf("%q", input), input))
	}
	describeGrammar := func(description string, body any) {
		DescribeTable(description, append([]any{body}, grammarCorpus...)...)
	}

	describeGrammar("checks a timeline name exactly as timeline.ValidateTimelineName does",
		func(ctx SpecContext, name string) {
			_, err := queries.CreateTimeline(ctx, sqlcgen.CreateTimelineParams{
				ID: id(10), Name: name, CreatedAt: now, UpdatedAt: now,
			})
			expectCheckAgrees(err, timeline.ValidateTimelineName(name))
		},
	)

	describeGrammar("checks an overlay name exactly as timeline.ValidateOverlayName does",
		func(ctx SpecContext, name string) {
			_, err := queries.CreateOverlay(ctx, sqlcgen.CreateOverlayParams{ID: id(10), Name: name, CreatedAt: now})
			expectCheckAgrees(err, timeline.ValidateOverlayName(name))
		},
	)

	describeGrammar("checks a source name exactly as timeline.ValidateSourceName does",
		func(ctx SpecContext, name string) {
			_, err := queries.CreateSource(ctx, sqlcgen.CreateSourceParams{ID: id(10), Name: name, CreatedAt: now})
			expectCheckAgrees(err, timeline.ValidateSourceName(name))
		},
	)

	describeGrammar("checks a tag exactly as timeline.ValidateTag does",
		func(ctx SpecContext, tag string) {
			err := queries.AddEventTag(ctx, sqlcgen.AddEventTagParams{EventID: pager.ID, Tag: tag})
			expectCheckAgrees(err, timeline.ValidateTag(tag))
		},
	)

	expectEnumAgrees := func(err error, valid bool) {
		GinkgoHelper()
		if valid {
			Expect(err).NotTo(HaveOccurred())
			return
		}
		Expect(err).To(MatchError(checkFailed))
	}

	DescribeTable("checks a precision exactly as timeline.Precision.Valid does",
		func(ctx SpecContext, precision timeline.Precision) {
			params := eventParams(10, 2)
			params.OccurredAt = timeline.FormatInstant(time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
			params.OccurredPrecision = string(precision)
			_, err := queries.CreateEvent(ctx, params)
			expectEnumAgrees(err, precision.Valid())
		},
		Entry("day", timeline.PrecisionDay), Entry("hour", timeline.PrecisionHour),
		Entry("minute", timeline.PrecisionMinute), Entry("second", timeline.PrecisionSecond),
		Entry("millisecond", timeline.PrecisionMillisecond), Entry("microsecond", timeline.PrecisionMicrosecond),
		Entry("nanosecond", timeline.PrecisionNanosecond),
		Entry("fortnight", timeline.Precision("fortnight")), Entry("Day", timeline.Precision("Day")),
		Entry("empty", timeline.Precision("")),
	)

	DescribeTable("checks an annotation kind exactly as timeline.AnnotationKind.Valid does",
		func(ctx SpecContext, kind timeline.AnnotationKind) {
			params := annotationParams(10, incident.ID, &pager.ID)
			params.Kind = string(kind)
			_, err := queries.CreateAnnotation(ctx, params)
			expectEnumAgrees(err, kind.Valid())
		},
		Entry("evidence", timeline.AnnotationEvidence), Entry("comment", timeline.AnnotationComment),
		Entry("note", timeline.AnnotationKind("note")), Entry("empty", timeline.AnnotationKind("")),
	)

	DescribeTable("checks a link kind exactly as timeline.LinkKind.Valid does",
		func(ctx SpecContext, kind timeline.LinkKind) {
			params := linkParams(10, secondEvent(ctx))
			params.Kind = string(kind)
			_, err := queries.CreateLink(ctx, params)
			expectEnumAgrees(err, kind.Valid())
		},
		Entry("led_to", timeline.LinkLedTo), Entry("same_as", timeline.LinkSameAs),
		Entry("copied_from", timeline.LinkCopiedFrom),
		Entry("after", timeline.LinkKind("after")), Entry("empty", timeline.LinkKind("")),
	)

	It("accepts an annotation on an event in its own timeline", func(ctx SpecContext) {
		Expect(queries.CreateAnnotation(ctx, annotationParams(10, incident.ID, &pager.ID))).
			To(HaveField("EventID", HaveValue(Equal(pager.ID))))
	})

	It("accepts an annotation on the timeline itself", func(ctx SpecContext) {
		Expect(queries.CreateAnnotation(ctx, annotationParams(10, other.ID, nil))).
			To(HaveField("EventID", BeNil()))
	})

	It("accepts every precision", func(ctx SpecContext) {
		for n, precision := range []timeline.Precision{
			timeline.PrecisionDay, timeline.PrecisionHour, timeline.PrecisionMinute, timeline.PrecisionSecond,
			timeline.PrecisionMillisecond, timeline.PrecisionMicrosecond, timeline.PrecisionNanosecond,
		} {
			params := eventParams(10+n, int64(2+n))
			params.OccurredPrecision = string(precision)
			Expect(queries.CreateEvent(ctx, params)).Error().NotTo(HaveOccurred(), string(precision))
		}
	})

	It("accepts every annotation kind", func(ctx SpecContext) {
		for n, kind := range []timeline.AnnotationKind{timeline.AnnotationEvidence, timeline.AnnotationComment} {
			params := annotationParams(10+n, incident.ID, nil)
			params.Kind = string(kind)
			Expect(queries.CreateAnnotation(ctx, params)).Error().NotTo(HaveOccurred(), string(kind))
		}
	})

	It("accepts every link kind", func(ctx SpecContext) {
		to := secondEvent(ctx)
		for n, kind := range []timeline.LinkKind{timeline.LinkLedTo, timeline.LinkSameAs, timeline.LinkCopiedFrom} {
			params := linkParams(10+n, to)
			params.Kind = string(kind)
			Expect(queries.CreateLink(ctx, params)).Error().NotTo(HaveOccurred(), string(kind))
		}
	})

	It("deletes a timeline's tags, refs and annotations with it", func(ctx SpecContext) {
		Expect(queries.AddEventTag(ctx, sqlcgen.AddEventTagParams{EventID: pager.ID, Tag: "phase:detect"})).To(Succeed())
		Expect(queries.AddTimelineRef(ctx, sqlcgen.AddTimelineRefParams{TimelineID: incident.ID, Ref: "INC-465"})).To(Succeed())
		Expect(queries.CreateAnnotation(ctx, annotationParams(10, incident.ID, &pager.ID))).Error().NotTo(HaveOccurred())
		Expect(queries.CreateAnnotation(ctx, annotationParams(11, incident.ID, nil))).Error().NotTo(HaveOccurred())

		Expect(queries.DeleteTimeline(ctx, incident.ID)).To(Succeed())

		Expect(queries.ListEventTags(ctx, pager.ID)).To(BeEmpty())
		Expect(queries.ListTimelineRefs(ctx, incident.ID)).To(BeEmpty())
		Expect(queries.ListAnnotations(ctx, incident.ID)).To(BeEmpty())
	})

	It("deletes an event's attachments, and those of its annotations, with it", func(ctx SpecContext) {
		annotation, err := queries.CreateAnnotation(ctx, annotationParams(10, incident.ID, &pager.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(queries.CreateAttachment(ctx, attachmentParams(11, &pager.ID, nil))).Error().NotTo(HaveOccurred())
		Expect(queries.CreateAttachment(ctx, attachmentParams(12, nil, &annotation.ID))).Error().NotTo(HaveOccurred())

		Expect(queries.DeleteEvent(ctx, pager.ID)).To(Succeed())

		Expect(queries.ListEventAttachments(ctx, &pager.ID)).To(BeEmpty())
		Expect(queries.ListAnnotationAttachments(ctx, &annotation.ID)).To(BeEmpty())
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
