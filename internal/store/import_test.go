package store_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Import", func() {
	created := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	quarterPast := time.Date(2026, 9, 12, 14, 15, 0, 0, time.UTC)
	incident := timeline.Timeline{
		ID:        uuid(1),
		Name:      "inc-465",
		Title:     "db-2 primary flapped",
		Refs:      []string{"INC-465", "https://example.com/browse/INC-465"},
		CreatedAt: created,
		UpdatedAt: created,
	}
	pagerduty := timeline.Source{ID: uuid(20), Name: "pagerduty", CreatedAt: created}
	slack := timeline.Source{ID: uuid(21), Name: "slack", CreatedAt: created.Add(time.Minute)}
	screenshot := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR a Grafana panel")
	screenshotSHA256 := func() string {
		sum := sha256.Sum256(screenshot)
		return hex.EncodeToString(sum[:])
	}()

	newEvent := func(n int, seq int64) timeline.Event {
		return timeline.Event{
			ID:         uuid(n),
			TimelineID: incident.ID,
			Seq:        seq,
			OccurredAt: quarterPast.Add(time.Duration(n) * time.Minute),
			Precision:  timeline.PrecisionMinute,
			Tags:       []string{},
			Text:       "Pager fires for db-2",
			CreatedAt:  created,
			UpdatedAt:  created,
		}
	}

	var (
		s       *store.Store
		dbPath  string
		blobDir string
	)

	BeforeEach(func(ctx SpecContext) {
		dbPath = filepath.Join(GinkgoT().TempDir(), "coroner.db")
		blobDir = config.BlobDir(dbPath)
		var err error
		s, err = store.Open(ctx, dbPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
	})

	It("writes records into an empty store under the ids, seqs, authors and instants they carry",
		func(ctx SpecContext) {
			pager := newEvent(10, 1)
			pager.OccurredOffset = new(-4 * 3600)
			pager.Precision = timeline.PrecisionSecond
			pager.Approximate = true
			pager.OccurredRaw = new("around 10:15")
			pager.EndedAt = new(pager.OccurredAt.Add(20 * time.Minute))
			pager.Source = new(pagerduty.Name)
			pager.Tags = []string{"phase:detect", "team:db"}
			pager.URL = new("https://example.com/incidents/465")
			pager.Author = new("julian")
			pager.CreatedAt = created.Add(time.Hour)
			pager.UpdatedAt = created.Add(2 * time.Hour)
			events := []timeline.Event{pager, newEvent(11, 2), newEvent(12, 3)}
			onPager := timeline.Annotation{
				ID: uuid(30), TimelineID: incident.ID, EventID: new(pager.ID), Kind: timeline.AnnotationEvidence,
				Body: "mongod: election timeout after 10000ms", Author: new("sam"), Source: new(slack.Name),
				URL: new("https://example.com/archives/C1/p1"), CreatedAt: created, UpdatedAt: created.Add(time.Hour),
			}
			onIncident := timeline.Annotation{
				ID: uuid(31), TimelineID: incident.ID, Kind: timeline.AnnotationComment,
				Body: "the election timeout is too short", CreatedAt: created.Add(time.Minute), UpdatedAt: created.Add(time.Minute),
			}

			Expect(s.Import(ctx, store.ImportRecords{
				Sources:     []timeline.Source{pagerduty, slack},
				Timelines:   []timeline.Timeline{incident},
				Events:      events,
				Annotations: []timeline.Annotation{onPager, onIncident},
			})).Error().To(Succeed())

			Expect(s.Timeline(ctx, incident.ID)).To(Equal(incident))
			Expect(s.Events(ctx, incident.ID)).To(Equal(events))
			Expect(s.Annotations(ctx, incident.ID)).To(Equal([]timeline.Annotation{onPager, onIncident}))
			Expect(s.Sources(ctx)).To(Equal([]timeline.Source{pagerduty, slack}))
		})

	It("gives an event added after an import the seq past the highest imported one", func(ctx SpecContext) {
		Expect(s.Import(ctx, store.ImportRecords{
			Timelines: []timeline.Timeline{incident},
			Events:    []timeline.Event{newEvent(12, 5), newEvent(10, 1), newEvent(11, 2)},
		})).Error().To(Succeed())

		added, err := s.AddEvent(ctx, newEvent(13, 0), uuid(20))

		Expect(err).NotTo(HaveOccurred())
		Expect(added.Seq).To(Equal(int64(6)))
	})

	It("gives an imported event with no seq the timeline's next seq after keeping the others'", func(ctx SpecContext) {
		Expect(s.Import(ctx, store.ImportRecords{
			Timelines: []timeline.Timeline{incident},
			Events:    []timeline.Event{newEvent(10, 0), newEvent(11, 1), newEvent(12, 2)},
		})).Error().To(Succeed())

		Expect(s.Event(ctx, uuid(11))).To(HaveField("Seq", int64(1)))
		Expect(s.Event(ctx, uuid(12))).To(HaveField("Seq", int64(2)))
		Expect(s.Event(ctx, uuid(10))).To(HaveField("Seq", int64(3)))
	})

	It("renumbers an imported event whose seq the timeline has already given, and keeps one past it",
		func(ctx SpecContext) {
			Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
			for n := range 2 {
				Expect(s.AddEvent(ctx, newEvent(10+n, 0), uuid(20))).Error().NotTo(HaveOccurred())
			}

			Expect(s.Import(ctx, store.ImportRecords{
				Events: []timeline.Event{newEvent(13, 7), newEvent(12, 2)},
			})).Error().To(Succeed())

			Expect(s.Event(ctx, uuid(12))).To(HaveField("Seq", int64(3)))
			Expect(s.Event(ctx, uuid(13))).To(HaveField("Seq", int64(7)))
		})

	It("returns the events it inserted, each with the seq it was written under", func(ctx SpecContext) {
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		Expect(s.AddEvent(ctx, newEvent(10, 0), uuid(20))).Error().NotTo(HaveOccurred())

		inserted, err := s.Import(ctx, store.ImportRecords{
			Events: []timeline.Event{newEvent(11, 7), newEvent(12, 1), newEvent(13, 0)},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(inserted).To(HaveLen(3))
		for _, e := range inserted {
			Expect(s.Event(ctx, e.ID)).To(HaveField("Seq", e.Seq))
		}
	})

	It("replaces every replaceable field of a timeline, event and annotation, keeping the event's seq",
		func(ctx SpecContext) {
			pager := newEvent(10, 1)
			pager.Source = new(pagerduty.Name)
			pager.Tags = []string{"phase:detect", "team:db"}
			evidence := timeline.Annotation{
				ID: uuid(30), TimelineID: incident.ID, EventID: new(pager.ID), Kind: timeline.AnnotationEvidence,
				Body: "mongod: election timeout after 10000ms", CreatedAt: created, UpdatedAt: created,
			}
			Expect(s.Import(ctx, store.ImportRecords{
				Sources:     []timeline.Source{pagerduty},
				Timelines:   []timeline.Timeline{incident},
				Events:      []timeline.Event{pager},
				Annotations: []timeline.Annotation{evidence},
			})).Error().To(Succeed())
			later := created.Add(24 * time.Hour)
			renamed := timeline.Timeline{
				ID: incident.ID, Name: "inc-465-db2", Title: "db-2 election storm",
				Refs: []string{"OPS-42", "INC-465"}, CreatedAt: created.Add(-time.Hour), UpdatedAt: later,
			}
			edited := timeline.Event{
				ID: pager.ID, TimelineID: incident.ID, Seq: 9, OccurredAt: quarterPast.Add(-time.Hour),
				OccurredOffset: new(3600), Precision: timeline.PrecisionSecond, Approximate: true,
				OccurredRaw: new("an hour earlier"), EndedAt: new(quarterPast), Source: new(slack.Name),
				Tags: []string{"phase:respond"}, Text: "db-2 steps down", URL: new("https://example.com/p/2"),
				Author: new("sam"), CreatedAt: created.Add(-time.Hour), UpdatedAt: later,
			}
			revised := timeline.Annotation{
				ID: evidence.ID, TimelineID: incident.ID, EventID: evidence.EventID, Kind: timeline.AnnotationComment,
				Body: "the timeout is too short", Author: new("sam"), Source: new(slack.Name),
				URL: new("https://example.com/p/3"), CreatedAt: created.Add(-time.Hour), UpdatedAt: later,
			}

			Expect(s.Import(ctx, store.ImportRecords{
				Sources:                []timeline.Source{slack},
				TimelineReplacements:   []timeline.Timeline{renamed},
				EventReplacements:      []timeline.Event{edited},
				AnnotationReplacements: []timeline.Annotation{revised},
			})).Error().To(Succeed())

			edited.Seq = 1
			Expect(s.Timeline(ctx, incident.ID)).To(Equal(renamed))
			Expect(s.Event(ctx, pager.ID)).To(Equal(edited))
			Expect(s.Annotation(ctx, evidence.ID)).To(Equal(revised))
		})

	DescribeTable("returns ErrNotFound when a replacement names a record that is not stored",
		func(ctx SpecContext, records store.ImportRecords) {
			Expect(s.Import(ctx, store.ImportRecords{
				Timelines: []timeline.Timeline{incident},
				Events:    []timeline.Event{newEvent(10, 1)},
			})).Error().To(Succeed())

			Expect(s.Import(ctx, records)).Error().To(MatchError(store.ErrNotFound))
		},
		Entry("a timeline replacement", store.ImportRecords{
			TimelineReplacements: []timeline.Timeline{{
				ID: uuid(2), Name: "inc-466", Refs: []string{}, CreatedAt: created, UpdatedAt: created,
			}},
		}),
		Entry("an event replacement", store.ImportRecords{
			EventReplacements: []timeline.Event{newEvent(11, 2)},
		}),
		Entry("an annotation replacement", store.ImportRecords{
			AnnotationReplacements: []timeline.Annotation{{
				ID: uuid(30), TimelineID: incident.ID, EventID: new(uuid(10)), Kind: timeline.AnnotationComment,
				Body: "the timeout is too short", CreatedAt: created, UpdatedAt: created,
			}},
		}),
	)

	Describe("attachments", func() {
		pager := newEvent(10, 1)
		onPager := timeline.AttachmentOwner{EventID: new(pager.ID)}
		grafana := timeline.Attachment{
			ID: uuid(40), Owner: onPager, Filename: "grafana.png", MIME: "application/octet-stream",
			SHA256: screenshotSHA256, Size: int64(len(screenshot)), CreatedAt: created,
		}
		withEvent := func(attachments ...timeline.Attachment) store.ImportRecords {
			return store.ImportRecords{
				Timelines: []timeline.Timeline{incident}, Events: []timeline.Event{pager}, Attachments: attachments,
			}
		}

		It("stores content under its hash, keeping the record's MIME", func(ctx SpecContext) {
			Expect(s.Import(ctx, withEvent(grafana), bytes.NewReader(screenshot))).Error().To(Succeed())

			Expect(s.ListAttachments(ctx, onPager)).To(Equal([]timeline.Attachment{grafana}))
			Expect(os.ReadFile(filepath.Clean(store.BlobPath(dbPath, screenshotSHA256)))).To(Equal(screenshot))
		})

		It("inserts an attachment without content whose blob is already stored", func(ctx SpecContext) {
			other := timeline.Timeline{ID: uuid(2), Name: "inc-466", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
			Expect(s.CreateTimeline(ctx, other)).To(Succeed())
			earlier := newEvent(11, 0)
			earlier.TimelineID = other.ID
			Expect(s.AddEvent(ctx, earlier, uuid(20))).Error().NotTo(HaveOccurred())
			Expect(attach(ctx, s, timeline.Attachment{
				ID: uuid(41), Owner: timeline.AttachmentOwner{EventID: new(earlier.ID)}, Filename: "panel.png", CreatedAt: created,
			}, bytes.NewReader(screenshot))).Error().NotTo(HaveOccurred())

			Expect(s.Import(ctx, withEvent(grafana))).Error().To(Succeed())

			Expect(s.ListAttachments(ctx, onPager)).To(Equal([]timeline.Attachment{grafana}))
		})

		DescribeTable("writes nothing when an attachment's content cannot be stored",
			func(ctx SpecContext, attachment timeline.Attachment, content []byte, matchErr types.GomegaMatcher) {
				var blobs []io.Reader
				if content != nil {
					blobs = append(blobs, bytes.NewReader(content))
				}
				Expect(s.Import(ctx, withEvent(attachment), blobs...)).Error().To(matchErr)

				Expect(s.Event(ctx, pager.ID)).Error().To(MatchError(store.ErrNotFound))
				Expect(filepath.Glob(filepath.Join(blobDir, "*"))).To(BeEmpty())
			},
			Entry("content whose hash differs from the record's, with ErrNotFound",
				grafana, bytes.ToUpper(screenshot), MatchError(store.ErrNotFound)),
			Entry("content whose size differs from the record's", func() timeline.Attachment {
				short := grafana
				short.Size--
				return short
			}(), screenshot, HaveOccurred()),
			Entry("no content and no stored blob, with ErrNotFound", grafana, nil, MatchError(store.ErrNotFound)),
		)
	})

	It("writes links between the events it writes", func(ctx SpecContext) {
		link := timeline.Link{
			ID: uuid(50), FromEventID: uuid(10), ToEventID: uuid(11), Kind: timeline.LinkLedTo, CreatedAt: created, Author: new("sam"),
		}

		Expect(s.Import(ctx, store.ImportRecords{
			Timelines: []timeline.Timeline{incident},
			Events:    []timeline.Event{newEvent(10, 1), newEvent(11, 2)},
			Links:     []timeline.Link{link},
		})).Error().To(Succeed())

		Expect(s.EventLinks(ctx, uuid(11))).To(Equal([]timeline.Link{link}))
	})

	It("writes nothing when a link's event is missing", func(ctx SpecContext) {
		_, err := s.Import(ctx, store.ImportRecords{
			Timelines: []timeline.Timeline{incident},
			Events:    []timeline.Event{newEvent(10, 1)},
			Links:     []timeline.Link{{ID: uuid(50), FromEventID: uuid(10), ToEventID: uuid(11), Kind: timeline.LinkSameAs, CreatedAt: created}},
		})

		Expect(err).To(MatchError(store.ErrNotFound))
		Expect(s.Timelines(ctx)).To(BeEmpty())
	})

	It("writes nothing when a write midway fails", func(ctx SpecContext) {
		twin := timeline.Timeline{ID: uuid(2), Name: incident.Name, Refs: []string{}, CreatedAt: created, UpdatedAt: created}

		_, err := s.Import(ctx, store.ImportRecords{
			Timelines: []timeline.Timeline{incident, twin},
			Events:    []timeline.Event{newEvent(10, 1)},
		})

		Expect(err).To(MatchError(store.ErrConflict))
		Expect(s.Timelines(ctx)).To(BeEmpty())
		Expect(s.Event(ctx, uuid(10))).Error().To(MatchError(store.ErrNotFound))
	})
})
