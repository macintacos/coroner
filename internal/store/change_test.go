package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

const changeIncidentName = "inc-465"

func changeCreated() time.Time { return time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC) }

func changeIncident() timeline.Timeline {
	return timeline.Timeline{ID: uuid(1), Name: changeIncidentName, Refs: []string{}, CreatedAt: changeCreated(), UpdatedAt: changeCreated()}
}

func changeEvent(n int) timeline.Event {
	return timeline.Event{
		ID:         uuid(n),
		TimelineID: uuid(1),
		OccurredAt: time.Date(2026, 9, 12, 14, 15, 0, 0, time.UTC),
		Precision:  timeline.PrecisionMinute,
		Tags:       []string{},
		Text:       "Pager fires for db-2",
		CreatedAt:  changeCreated(),
		UpdatedAt:  changeCreated(),
	}
}

// openStoreAt opens a store at a fresh path, closed when the spec ends, and returns the path.
func openStoreAt(ctx context.Context) (*store.Store, string) {
	GinkgoHelper()
	path := newDatabase()
	s, err := store.Open(ctx, path)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(s.Close)
	return s, path
}

func blobEntries(dbPath string) []os.DirEntry {
	GinkgoHelper()
	entries, err := os.ReadDir(config.BlobDir(dbPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	Expect(err).NotTo(HaveOccurred())
	return entries
}

var _ = Describe("Change", func() {
	commit := store.ChangeOptions{Mode: store.Commit}
	rollback := store.ChangeOptions{Mode: store.Rollback}
	content := func() []store.NewAttachment {
		return []store.NewAttachment{{ID: uuid(50), Filename: "panel.png", CreatedAt: changeCreated(), Content: bytes.NewReader([]byte("a Grafana panel"))}}
	}

	It("commits what fn writes under Commit", func(ctx SpecContext) {
		s := openStore(ctx)

		_, err := s.Change(ctx, commit, func(tx *store.Tx) error { return tx.CreateTimeline(ctx, changeIncident()) })

		Expect(err).NotTo(HaveOccurred())
		Expect(s.Timelines(ctx)).To(Equal([]timeline.Timeline{changeIncident()}))
	})

	It("leaves the database as it was under Rollback, and returns fn's results", func(ctx SpecContext) {
		s := openStore(ctx)

		var read timeline.Event
		_, err := s.Change(ctx, rollback, func(tx *store.Tx) error {
			err := tx.CreateTimeline(ctx, changeIncident())
			if err != nil {
				return err
			}
			added, err := tx.AddEvent(ctx, changeEvent(2), uuid(3))
			if err != nil {
				return err
			}
			read, err = tx.Event(ctx, added.ID)
			return err
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(read.Seq).To(Equal(int64(1)))
		Expect(s.Timelines(ctx)).To(BeEmpty())
	})

	It("gives a rolled-back event a seq the next committed event takes again", func(ctx SpecContext) {
		s := openStore(ctx)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())

		var rolledBack timeline.Event
		_, err := s.Change(ctx, rollback, func(tx *store.Tx) error {
			var err error
			rolledBack, err = tx.AddEvent(ctx, changeEvent(2), uuid(3))
			return err
		})
		Expect(err).NotTo(HaveOccurred())

		committed, err := s.AddEvent(ctx, changeEvent(4), uuid(5))
		Expect(err).NotTo(HaveOccurred())
		Expect(committed.Seq).To(Equal(rolledBack.Seq))
	})

	It("lets fn handle ErrConflict and keep reading in the same transaction", func(ctx SpecContext) {
		s := openStore(ctx)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())

		var conflict error
		var found timeline.Timeline
		_, err := s.Change(ctx, commit, func(tx *store.Tx) error {
			conflict = tx.CreateTimeline(ctx, timeline.Timeline{
				ID: uuid(9), Name: changeIncident().Name, Refs: []string{}, CreatedAt: changeCreated(), UpdatedAt: changeCreated(),
			})
			var err error
			found, err = tx.TimelineByName(ctx, changeIncident().Name)
			return err
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(conflict).To(MatchError(store.ErrConflict))
		Expect(found).To(Equal(changeIncident()))
	})

	It("names no blob and leaves no staged file under Rollback", func(ctx SpecContext) {
		s, path := openStoreAt(ctx)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())

		rows, err := s.Change(ctx, store.ChangeOptions{Mode: store.Rollback, Attach: content()}, func(tx *store.Tx) error {
			_, err := tx.AddEvent(ctx, changeEvent(2), uuid(3))
			tx.Attach(timeline.AttachmentOwner{EventID: new(uuid(2))})
			return err
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(BeEmpty())
		Expect(blobEntries(path)).To(BeEmpty())
	})

	It("names no blob when fn fails", func(ctx SpecContext) {
		s, path := openStoreAt(ctx)
		failed := errors.New("fn failed")

		_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Attach: content()}, func(tx *store.Tx) error {
			tx.Attach(timeline.AttachmentOwner{EventID: new(uuid(2))})
			return failed
		})

		Expect(err).To(MatchError(failed))
		Expect(blobEntries(path)).To(BeEmpty())
	})

	It("attaches nothing when fn never calls Attach", func(ctx SpecContext) {
		s, path := openStoreAt(ctx)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())

		rows, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Attach: content()}, func(tx *store.Tx) error {
			_, err := tx.AddEvent(ctx, changeEvent(2), uuid(3))
			return err
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(BeEmpty())
		Expect(s.ListAttachments(ctx, timeline.AttachmentOwner{EventID: new(uuid(2))})).To(BeEmpty())
		Expect(blobEntries(path)).To(BeEmpty())
	})

	It("returns the rows it attached under Commit", func(ctx SpecContext) {
		s := openStore(ctx)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())
		owner := timeline.AttachmentOwner{EventID: new(uuid(2))}

		rows, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Attach: content()}, func(tx *store.Tx) error {
			_, err := tx.AddEvent(ctx, changeEvent(2), uuid(3))
			tx.Attach(owner)
			return err
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(1))
		Expect(s.ListAttachments(ctx, owner)).To(Equal(rows))
	})

	{
		panel := []byte("a Grafana panel")
		trace := []byte("a trace")
		traceSHA256 := func() string {
			sum := sha256.Sum256(trace)
			return hex.EncodeToString(sum[:])
		}()
		blobs := func() []io.Reader {
			return []io.Reader{bytes.NewReader(panel), bytes.NewReader(panel), bytes.NewReader(trace)}
		}
		attachment := func(n int, sha string) timeline.Attachment {
			return timeline.Attachment{
				ID: uuid(n), Owner: timeline.AttachmentOwner{EventID: new(uuid(2))}, Filename: "trace.txt",
				MIME: "text/plain", SHA256: sha, Size: int64(len(trace)), CreatedAt: changeCreated(),
			}
		}
		// seeded opens a store holding one event, uuid(2), and returns its path.
		seeded := func(ctx context.Context) (*store.Store, string) {
			GinkgoHelper()
			s, path := openStoreAt(ctx)
			Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())
			Expect(s.AddEvent(ctx, changeEvent(2), uuid(3))).Error().NotTo(HaveOccurred())
			return s, path
		}
		importing := func(ctx context.Context, attachments ...timeline.Attachment) func(*store.Tx) error {
			return func(tx *store.Tx) error {
				_, err := tx.Import(ctx, store.ImportRecords{Attachments: attachments})
				return err
			}
		}

		It("names under Commit only the Blobs an imported attachment carries, and discards the rest", func(ctx SpecContext) {
			s, path := seeded(ctx)

			_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Blobs: blobs()}, importing(ctx, attachment(60, traceSHA256)))

			Expect(err).NotTo(HaveOccurred())
			entries := blobEntries(path)
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Name()).To(Equal(traceSHA256))
		})

		It("names no Blobs under Rollback", func(ctx SpecContext) {
			s, path := seeded(ctx)

			_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Rollback, Blobs: blobs()}, importing(ctx, attachment(60, traceSHA256)))

			Expect(err).NotTo(HaveOccurred())
			Expect(blobEntries(path)).To(BeEmpty())
		})

		It("names no Blobs when fn fails", func(ctx SpecContext) {
			s, path := seeded(ctx)
			failed := errors.New("fn failed")

			_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Blobs: blobs()}, func(tx *store.Tx) error {
				Expect(importing(ctx, attachment(60, traceSHA256))(tx)).To(Succeed())
				return failed
			})

			Expect(err).To(MatchError(failed))
			Expect(blobEntries(path)).To(BeEmpty())
		})

		It("names nothing for a Tx.Import that fails and fn recovers from", func(ctx SpecContext) {
			s, path := seeded(ctx)
			var importErr error

			_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit, Blobs: blobs()}, func(tx *store.Tx) error {
				importErr = importing(ctx, attachment(60, traceSHA256), attachment(61, strings.Repeat("0", 64)))(tx)
				return nil
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(importErr).To(MatchError(store.ErrNotFound))
			Expect(blobEntries(path)).To(BeEmpty())
			Expect(s.ListAttachments(ctx, timeline.AttachmentOwner{EventID: new(uuid(2))})).To(BeEmpty())
		})
	}

	It("serialises: a write on a second connection waits for the Change, or gets ErrBusy", func(ctx SpecContext) {
		s, path := openStoreAt(ctx)
		other, err := store.Open(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(other.Close)

		inside, release, changed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			_, err := s.Change(ctx, commit, func(tx *store.Tx) error {
				err := tx.CreateTimeline(ctx, changeIncident())
				close(inside)
				<-release
				return err
			})
			changed <- err
		}()
		Eventually(ctx, inside).Should(BeClosed())

		created := make(chan error, 1)
		go func() {
			created <- other.CreateTimeline(ctx, timeline.Timeline{
				ID: uuid(9), Name: "inc-466", Refs: []string{}, CreatedAt: changeCreated(), UpdatedAt: changeCreated(),
			})
		}()
		Consistently(created).WithTimeout(500 * time.Millisecond).ShouldNot(Receive())

		close(release)
		Eventually(ctx, changed).Should(Receive(BeNil()))
		Eventually(ctx, created).Should(Receive(Or(BeNil(), MatchError(store.ErrBusy))))
	}, SpecTimeout(time.Minute))
})

var _ = Describe("Read", func() {
	It("sees one snapshot while another connection writes", func(ctx SpecContext) {
		s, path := openStoreAt(ctx)
		other, err := store.Open(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(other.Close)

		var before, after []timeline.Timeline
		err = s.Read(ctx, func(tx *store.Tx) error {
			var err error
			before, err = tx.Timelines(ctx)
			if err != nil {
				return err
			}
			err = other.CreateTimeline(ctx, changeIncident())
			if err != nil {
				return err
			}
			after, err = tx.Timelines(ctx)
			return err
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(HaveLen(len(before)))
		Expect(s.Timelines(ctx)).To(HaveLen(1))
	})
})

var _ = Describe("Tx", func() {
	missing := uuid(99)
	event := changeEvent(2)
	annotation := timeline.Annotation{
		ID: uuid(3), TimelineID: changeIncident().ID, EventID: new(missing), Kind: timeline.AnnotationEvidence,
		Body: "mongod: election timeout", CreatedAt: changeCreated(), UpdatedAt: changeCreated(),
	}
	missingTimeline := changeIncident()
	missingTimeline.ID = missing

	DescribeTable("gives ErrNotFound for a missing record",
		func(ctx SpecContext, call func(context.Context, *store.Tx) error) {
			s := openStore(ctx)
			Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())

			var callErr error
			_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Rollback}, func(tx *store.Tx) error {
				callErr = call(ctx, tx)
				return nil
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(errors.Is(callErr, store.ErrNotFound)).To(BeTrue(), "got %v", callErr)
		},
		Entry("Timeline", func(ctx context.Context, tx *store.Tx) error { _, err := tx.Timeline(ctx, missing); return err }),
		Entry("TimelineByName", func(ctx context.Context, tx *store.Tx) error {
			_, err := tx.TimelineByName(ctx, "inc-missing")
			return err
		}),
		Entry("Event", func(ctx context.Context, tx *store.Tx) error { _, err := tx.Event(ctx, missing); return err }),
		Entry("EventBySeq", func(ctx context.Context, tx *store.Tx) error {
			_, err := tx.EventBySeq(ctx, changeIncident().ID, 7)
			return err
		}),
		Entry("Annotation", func(ctx context.Context, tx *store.Tx) error { _, err := tx.Annotation(ctx, missing); return err }),
		Entry("Attachment", func(ctx context.Context, tx *store.Tx) error { _, err := tx.Attachment(ctx, missing); return err }),
		Entry("Link", func(ctx context.Context, tx *store.Tx) error { _, err := tx.Link(ctx, missing); return err }),
		Entry("LinkBetween", func(ctx context.Context, tx *store.Tx) error {
			_, err := tx.LinkBetween(ctx, missing, uuid(98), timeline.LinkLedTo)
			return err
		}),
		Entry("OverlayByName", func(ctx context.Context, tx *store.Tx) error {
			_, err := tx.OverlayByName(ctx, "missing")
			return err
		}),
		Entry("UpdateEvent", func(ctx context.Context, tx *store.Tx) error { return tx.UpdateEvent(ctx, event, uuid(4)) }),
		Entry("UpdateAnnotation", func(ctx context.Context, tx *store.Tx) error {
			return tx.UpdateAnnotation(ctx, annotation, uuid(4))
		}),
		Entry("UpdateTimeline", func(ctx context.Context, tx *store.Tx) error { return tx.UpdateTimeline(ctx, missingTimeline) }),
		Entry("DeleteTimeline", func(ctx context.Context, tx *store.Tx) error { return tx.DeleteTimeline(ctx, missing) }),
		Entry("SetCurrentTimeline", func(ctx context.Context, tx *store.Tx) error { return tx.SetCurrentTimeline(ctx, missing) }),
		Entry("DeleteLink", func(ctx context.Context, tx *store.Tx) error { return tx.DeleteLink(ctx, missing) }),
		Entry("RenameSource", func(ctx context.Context, tx *store.Tx) error { return tx.RenameSource(ctx, missing, "pagerduty") }),
		Entry("DeleteEvents", func(ctx context.Context, tx *store.Tx) error { return tx.DeleteEvents(ctx, missing) }),
		Entry("DeleteAnnotations", func(ctx context.Context, tx *store.Tx) error { return tx.DeleteAnnotations(ctx, missing) }),
		Entry("DeleteAttachments", func(ctx context.Context, tx *store.Tx) error { return tx.DeleteAttachments(ctx, missing) }),
		Entry("AddAnnotation, a missing target", func(ctx context.Context, tx *store.Tx) error {
			return tx.AddAnnotation(ctx, annotation, uuid(4))
		}),
		Entry("AddEvent, a missing timeline", func(ctx context.Context, tx *store.Tx) error {
			e := event
			e.TimelineID = missing
			_, err := tx.AddEvent(ctx, e, uuid(4))
			return err
		}),
		Entry("AddLink, a missing event", func(ctx context.Context, tx *store.Tx) error {
			return tx.AddLink(ctx, timeline.Link{ID: uuid(50), FromEventID: missing, ToEventID: uuid(98), Kind: timeline.LinkLedTo, CreatedAt: changeCreated()})
		}),
		Entry("CreateOverlay, a missing timeline", func(ctx context.Context, tx *store.Tx) error {
			return tx.CreateOverlay(ctx, timeline.Overlay{ID: uuid(5), Name: "pair", TimelineIDs: []string{changeIncident().ID, missing}, CreatedAt: changeCreated()})
		}),
	)

	// swallowing runs call in a committed Change whose fn ignores call's error.
	swallowing := func(ctx context.Context, s *store.Store, call func(*store.Tx) error) error {
		GinkgoHelper()
		var callErr error
		_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Commit}, func(tx *store.Tx) error {
			callErr = call(tx)
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
		return callErr
	}

	It("leaves no overlay behind when its timelines conflict", func(ctx SpecContext) {
		s := openStore(ctx)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())
		pair := timeline.Overlay{ID: uuid(5), Name: "pair", TimelineIDs: []string{uuid(1), uuid(1)}, CreatedAt: changeCreated()}

		err := swallowing(ctx, s, func(tx *store.Tx) error { return tx.CreateOverlay(ctx, pair) })

		Expect(errors.Is(err, store.ErrConflict)).To(BeTrue(), "got %v", err)
		_, err = s.OverlayByName(ctx, "pair")
		Expect(errors.Is(err, store.ErrNotFound)).To(BeTrue(), "got %v", err)
	})

	It("leaves no source or seq behind when an event's id is taken", func(ctx SpecContext) {
		s := openStore(ctx)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())
		_, err := s.AddEvent(ctx, event, uuid(4))
		Expect(err).NotTo(HaveOccurred())
		duplicate := event
		duplicate.Source = new("pagerduty")

		err = swallowing(ctx, s, func(tx *store.Tx) error { _, err := tx.AddEvent(ctx, duplicate, uuid(6)); return err })

		Expect(errors.Is(err, store.ErrConflict)).To(BeTrue(), "got %v", err)
		Expect(s.Sources(ctx)).To(BeEmpty())
		next, err := s.AddEvent(ctx, changeEvent(7), uuid(8))
		Expect(err).NotTo(HaveOccurred())
		Expect(next.Seq).To(Equal(int64(2)))
	})

	DescribeTable("gives ErrConflict for a write that collides",
		func(ctx SpecContext, call func(context.Context, *store.Tx) error) {
			s := openStore(ctx)
			Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())
			other := changeIncident()
			other.ID, other.Name = uuid(10), "inc-466"
			Expect(s.CreateTimeline(ctx, other)).To(Succeed())
			_, err := s.AddEvent(ctx, event, uuid(4))
			Expect(err).NotTo(HaveOccurred())
			second := changeEvent(11)
			second.Source = new("grafana")
			_, err = s.AddEvent(ctx, second, uuid(12))
			Expect(err).NotTo(HaveOccurred())
			third := changeEvent(13)
			third.Source = new("pagerduty")
			_, err = s.AddEvent(ctx, third, uuid(14))
			Expect(err).NotTo(HaveOccurred())
			Expect(s.CreateOverlay(ctx, timeline.Overlay{ID: uuid(15), Name: "pair", TimelineIDs: []string{uuid(1)}, CreatedAt: changeCreated()})).To(Succeed())
			Expect(s.AddLink(ctx, link(16))).To(Succeed())

			err = swallowing(ctx, s, func(tx *store.Tx) error { return call(ctx, tx) })

			Expect(errors.Is(err, store.ErrConflict)).To(BeTrue(), "got %v", err)
		},
		Entry("CreateTimeline, a taken name", func(ctx context.Context, tx *store.Tx) error {
			t := changeIncident()
			t.ID = uuid(20)
			return tx.CreateTimeline(ctx, t)
		}),
		Entry("UpdateTimeline, to a taken name", func(ctx context.Context, tx *store.Tx) error {
			t := changeIncident()
			t.ID = uuid(10)
			return tx.UpdateTimeline(ctx, t)
		}),
		Entry("CreateOverlay, a taken name", func(ctx context.Context, tx *store.Tx) error {
			return tx.CreateOverlay(ctx, timeline.Overlay{ID: uuid(21), Name: "pair", TimelineIDs: []string{uuid(1)}, CreatedAt: changeCreated()})
		}),
		Entry("AddLink, a duplicate link", func(ctx context.Context, tx *store.Tx) error { return tx.AddLink(ctx, link(22)) }),
		Entry("RenameSource, to a taken name", func(ctx context.Context, tx *store.Tx) error {
			return tx.RenameSource(ctx, uuid(12), "pagerduty")
		}),
		Entry("AddEvent, a taken id", func(ctx context.Context, tx *store.Tx) error { _, err := tx.AddEvent(ctx, event, uuid(23)); return err }),
	)
})

func link(n int) timeline.Link {
	return timeline.Link{ID: uuid(n), FromEventID: uuid(2), ToEventID: uuid(13), Kind: timeline.LinkLedTo, CreatedAt: changeCreated()}
}
