package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing/iotest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// attach stores content as a, the way a command does: staged by a Change that attaches it
// to a's owner.
func attach(ctx context.Context, s *store.Store, a timeline.Attachment, content io.Reader) (timeline.Attachment, error) {
	rows, err := s.Change(ctx, store.ChangeOptions{Attach: []store.NewAttachment{
		{ID: a.ID, Filename: a.Filename, CreatedAt: a.CreatedAt, Content: content},
	}}, func(tx *store.Tx) error {
		tx.Attach(a.Owner)
		return nil
	})
	if err != nil {
		return timeline.Attachment{}, err
	}

	return rows[0], nil
}

var _ = Describe("Attachments", func() {
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
	evidence := timeline.Annotation{
		ID:         uuid(3),
		TimelineID: incident.ID,
		EventID:    new(pager.ID),
		Kind:       timeline.AnnotationEvidence,
		Body:       "mongod: election timeout after 10000ms",
		CreatedAt:  created,
		UpdatedAt:  created,
	}
	onPager := timeline.AttachmentOwner{EventID: new(pager.ID)}
	onEvidence := timeline.AttachmentOwner{AnnotationID: new(evidence.ID)}
	screenshot := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR a Grafana panel")

	var (
		s       *store.Store
		dir     string
		dbPath  string
		blobDir string
	)

	newAttachment := func(n int, owner timeline.AttachmentOwner, filename string) timeline.Attachment {
		return timeline.Attachment{ID: uuid(n), Owner: owner, Filename: filename, CreatedAt: created.Add(time.Duration(n) * time.Second)}
	}
	sha256Hex := func(content []byte) string {
		sum := sha256.Sum256(content)
		return hex.EncodeToString(sum[:])
	}
	permissions := func(path string) os.FileMode {
		GinkgoHelper()
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		return info.Mode().Perm()
	}
	add := func(ctx context.Context, a timeline.Attachment, content []byte) timeline.Attachment {
		GinkgoHelper()
		added, err := attach(ctx, s, a, bytes.NewReader(content))
		Expect(err).NotTo(HaveOccurred())
		return added
	}

	BeforeEach(func(ctx SpecContext) {
		dir = GinkgoT().TempDir()
		dbPath = filepath.Join(dir, "coroner.db")
		blobDir = config.BlobDir(dbPath)
		var err error
		s, err = store.Open(ctx, dbPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		Expect(s.AddEvent(ctx, pager, uuid(9))).Error().NotTo(HaveOccurred())
		Expect(s.AddAnnotation(ctx, evidence, uuid(9))).To(Succeed())
	})

	It("stores a file under its content's hash, readable only by its owner, and lists it on its event",
		func(ctx SpecContext) {
			added := add(ctx, newAttachment(10, onPager, "grafana.png"), screenshot)

			want := newAttachment(10, onPager, "grafana.png")
			want.MIME = "image/png"
			want.SHA256 = sha256Hex(screenshot)
			want.Size = int64(len(screenshot))
			Expect(added).To(Equal(want))
			Expect(s.ListAttachments(ctx, onPager)).To(Equal([]timeline.Attachment{want}))

			blob := store.BlobPath(dbPath, want.SHA256)
			Expect(os.ReadFile(filepath.Clean(blob))).To(Equal(screenshot))
			Expect(permissions(blob)).To(Equal(os.FileMode(0o600)))
			Expect(permissions(blobDir)).To(Equal(os.FileMode(0o700)))
		})

	It("lists an annotation's attachments oldest first", func(ctx SpecContext) {
		later := add(ctx, newAttachment(11, onEvidence, "later.png"), screenshot)
		earlier := add(ctx, newAttachment(10, onEvidence, "earlier.png"), []byte("\x89PNG\r\n\x1a\nearlier"))

		Expect(s.ListAttachments(ctx, onEvidence)).To(Equal([]timeline.Attachment{earlier, later}))
		Expect(s.ListAttachments(ctx, onPager)).To(BeEmpty())
	})

	DescribeTable("takes the MIME type from the filename's extension when the content sniffs as generic text",
		func(ctx SpecContext, filename, content, want string) {
			Expect(add(ctx, newAttachment(10, onPager, filename), []byte(content))).To(HaveField("MIME", want))
		},
		Entry("JSON", "trace.json", `{"spans":[1,2]}`, "application/json"),
		Entry("SVG", "panel.svg", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`, "image/svg+xml"),
		Entry("an extension it does not know keeps the sniffed type",
			"mongod.log", "election timeout after 10000ms\n", "text/plain; charset=utf-8"),
	)

	It("keeps one blob for identical content until garbage collection finds no attachment using it",
		func(ctx SpecContext) {
			onEvent := add(ctx, newAttachment(10, onPager, "grafana.png"), screenshot)
			onAnnotation := add(ctx, newAttachment(11, onEvidence, "same-panel.png"), screenshot)
			blob := filepath.Join(blobDir, onEvent.SHA256)
			Expect(os.ReadDir(blobDir)).To(HaveExactElements(HaveField("Name()", onEvent.SHA256)))

			Expect(s.DeleteAttachments(ctx, onEvent.ID)).To(Succeed())
			Expect(s.GarbageCollectBlobs(ctx)).To(BeEmpty())
			Expect(blob).To(BeAnExistingFile())

			Expect(s.DeleteAttachments(ctx, onAnnotation.ID)).To(Succeed())
			Expect(s.GarbageCollectBlobs(ctx)).To(Equal([]string{onEvent.SHA256}))
			Expect(blob).NotTo(BeAnExistingFile())
		})

	It("deletes an event's attachments with it, and leaves their blobs to garbage collection", func(ctx SpecContext) {
		added := add(ctx, newAttachment(10, onPager, "grafana.png"), screenshot)

		Expect(s.DeleteEvents(ctx, pager.ID)).To(Succeed())

		Expect(s.ListAttachments(ctx, onPager)).To(BeEmpty())
		Expect(filepath.Join(blobDir, added.SHA256)).To(BeAnExistingFile())
		Expect(s.GarbageCollectBlobs(ctx)).To(Equal([]string{added.SHA256}))
	})

	It("leaves only an orphan blob, which garbage collection deletes, when the row cannot be written",
		func(ctx SpecContext) {
			first := add(ctx, newAttachment(10, onPager, "grafana.png"), screenshot)
			other := []byte("\x89PNG\r\n\x1a\nanother panel")

			_, err := attach(ctx, s, newAttachment(10, onPager, "other.png"), bytes.NewReader(other))

			Expect(err).To(MatchError(store.ErrConflict))
			Expect(s.ListAttachments(ctx, onPager)).To(Equal([]timeline.Attachment{first}))
			Expect(filepath.Join(blobDir, sha256Hex(other))).To(BeAnExistingFile())
			Expect(s.GarbageCollectBlobs(ctx)).To(Equal([]string{sha256Hex(other)}))
		})

	It("leaves nothing in the blob directory when reading the content fails", func(ctx SpecContext) {
		boom := errors.New("boom")
		content := io.MultiReader(bytes.NewReader(screenshot), iotest.ErrReader(boom))

		_, err := attach(ctx, s, newAttachment(10, onPager, "grafana.png"), content)

		Expect(err).To(MatchError(boom))
		Expect(os.ReadDir(blobDir)).To(BeEmpty())
	})

	It("never garbage-collects a file that is not a blob, such as a write still in progress", func(ctx SpecContext) {
		Expect(os.MkdirAll(blobDir, 0o700)).To(Succeed())
		inProgress := filepath.Join(blobDir, ".tmp-123")
		Expect(os.WriteFile(inProgress, screenshot, 0o600)).To(Succeed())

		Expect(s.GarbageCollectBlobs(ctx)).To(BeEmpty())
		Expect(inProgress).To(BeAnExistingFile())
	})

	It("never lets garbage collection delete a blob an attachment being added will use", func(ctx SpecContext) {
		const adds = 40
		var wg sync.WaitGroup
		done := make(chan struct{})
		wg.Go(func() {
			defer GinkgoRecover()
			defer close(done)
			for n := range adds {
				add(ctx, newAttachment(100+n, onPager, "panel.png"), []byte{byte(n)})
			}
		})
		wg.Go(func() {
			defer GinkgoRecover()
			for {
				select {
				case <-done:
					return
				default:
					_, err := s.GarbageCollectBlobs(ctx)
					Expect(err).NotTo(HaveOccurred())
					// SQLite's busy handler is not fair: a collector that never pauses starves the adder.
					time.Sleep(time.Millisecond)
				}
			}
		})
		wg.Wait()

		attachments, err := s.ListAttachments(ctx, onPager)
		Expect(err).NotTo(HaveOccurred())
		Expect(attachments).To(HaveLen(adds))
		for _, a := range attachments {
			Expect(filepath.Join(blobDir, a.SHA256)).To(BeAnExistingFile())
		}
	}, SpecTimeout(time.Minute))

	It("stores a filename that looks like a path as display text only", func(ctx SpecContext) {
		added := add(ctx, newAttachment(10, onPager, "../../escaped.png"), screenshot)

		Expect(s.ListAttachments(ctx, onPager)).To(HaveExactElements(HaveField("Filename", "../../escaped.png")))
		Expect(filepath.Join(dir, "..", "escaped.png")).NotTo(BeAnExistingFile())
		Expect(os.ReadDir(blobDir)).To(HaveExactElements(HaveField("Name()", added.SHA256)))
	})

	It("deletes none of the attachments when any id does not exist, reporting ErrNotFound", func(ctx SpecContext) {
		added := add(ctx, newAttachment(10, onPager, "grafana.png"), screenshot)

		Expect(s.DeleteAttachments(ctx, added.ID, uuid(99))).To(MatchError(store.ErrNotFound))
		Expect(s.ListAttachments(ctx, onPager)).To(Equal([]timeline.Attachment{added}))
	})

	Describe("written with their owner", func() {
		newFile := func(n int, filename string, content []byte) store.NewAttachment {
			return store.NewAttachment{
				ID: uuid(n), Filename: filename, CreatedAt: created.Add(time.Duration(n) * time.Second), Content: bytes.NewReader(content),
			}
		}
		trace := []byte("%PDF-1.7 trace of the election")
		described := func(n int, owner timeline.AttachmentOwner, filename, mime string, content []byte) timeline.Attachment {
			a := newAttachment(n, owner, filename)
			a.MIME, a.SHA256, a.Size = mime, sha256Hex(content), int64(len(content))
			return a
		}

		It("adds an event and its attachments together", func(ctx SpecContext) {
			stepDown := pager
			stepDown.ID = uuid(20)
			owner := timeline.AttachmentOwner{EventID: new(stepDown.ID)}

			_, err := s.AddEvent(ctx, stepDown, uuid(9), newFile(21, "grafana.png", screenshot), newFile(22, "trace.pdf", trace))

			Expect(err).NotTo(HaveOccurred())
			Expect(s.ListAttachments(ctx, owner)).To(Equal([]timeline.Attachment{
				described(21, owner, "grafana.png", "image/png", screenshot),
				described(22, owner, "trace.pdf", "application/pdf", trace),
			}))
			Expect(store.BlobPath(dbPath, sha256Hex(trace))).To(BeAnExistingFile())
		})

		It("adds neither the event nor its attachments when the event's row cannot be written", func(ctx SpecContext) {
			_, err := s.AddEvent(ctx, pager, uuid(9), newFile(21, "grafana.png", screenshot))

			Expect(err).To(MatchError(store.ErrConflict))
			Expect(s.ListAttachments(ctx, onPager)).To(BeEmpty())
			Expect(os.ReadDir(blobDir)).To(BeEmpty())
		})

		offsetBeyondADay := func() timeline.Event {
			e := pager
			e.ID, e.Seq, e.OccurredOffset = uuid(20), 5, new(timeline.MaxOccurredOffset+1)
			return e
		}
		eventsUnchanged := func(ctx context.Context) {
			Expect(s.Event(ctx, uuid(20))).Error().To(MatchError(store.ErrNotFound))
			Expect(s.Event(ctx, pager.ID)).To(HaveField("OccurredOffset", BeNil()))
			next := pager
			next.ID = uuid(40)
			Expect(s.AddEvent(ctx, next, uuid(9))).To(HaveField("Seq", int64(2)))
		}

		DescribeTable("rejects an invalid record, leaving no record and no file in the blob directory",
			func(ctx SpecContext, write func(context.Context) error, unchanged func(context.Context)) {
				_, ok := errors.AsType[*timeline.ValidationError](write(ctx))

				Expect(ok).To(BeTrue())
				unchanged(ctx)
				files, err := os.ReadDir(blobDir)
				Expect(err == nil || errors.Is(err, fs.ErrNotExist)).To(BeTrue(), "reading the blob directory: %v", err)
				Expect(files).To(BeEmpty())
			},
			Entry("AddEvent with an attachment", func(ctx context.Context) error {
				_, err := s.AddEvent(ctx, offsetBeyondADay(), uuid(9), newFile(21, "grafana.png", screenshot))
				return err
			}, eventsUnchanged),
			Entry("AddEvent with an attachment filename that is not UTF-8", func(ctx context.Context) error {
				e := pager
				e.ID, e.Seq = uuid(20), 5
				_, err := s.AddEvent(ctx, e, uuid(9), newFile(21, "caf\xe9.png", screenshot))
				return err
			}, eventsUnchanged),
			Entry("UpdateEvent with an attachment", func(ctx context.Context) error {
				e := offsetBeyondADay()
				e.ID, e.Seq = pager.ID, pager.Seq
				return s.UpdateEvent(ctx, e, uuid(9), newFile(21, "grafana.png", screenshot))
			}, eventsUnchanged),
			Entry("Import with an attachment", func(ctx context.Context) error {
				e := offsetBeyondADay()
				a := described(21, timeline.AttachmentOwner{EventID: new(e.ID)}, "grafana.png", "image/png", screenshot)
				_, err := s.Import(ctx, store.ImportRecords{
					Events:      []timeline.Event{e},
					Attachments: []timeline.Attachment{a},
				}, bytes.NewReader(screenshot))
				return err
			}, eventsUnchanged),
			Entry("AddAnnotation with an attachment", func(ctx context.Context) error {
				blank := evidence
				blank.ID, blank.Body = uuid(30), " "
				return s.AddAnnotation(ctx, blank, uuid(9), newFile(31, "trace.pdf", trace))
			}, func(ctx context.Context) {
				Expect(s.Annotation(ctx, uuid(30))).Error().To(MatchError(store.ErrNotFound))
			}),
			Entry("UpdateAnnotation with an attachment", func(ctx context.Context) error {
				blank := evidence
				blank.Body = " "
				return s.UpdateAnnotation(ctx, blank, uuid(9), newFile(31, "trace.pdf", trace))
			}, func(ctx context.Context) {
				Expect(s.Annotation(ctx, evidence.ID)).To(HaveField("Body", evidence.Body))
			}),
			Entry("AddLink", func(ctx context.Context) error {
				return s.AddLink(ctx, timeline.Link{
					ID: uuid(30), FromEventID: pager.ID, ToEventID: pager.ID, Kind: timeline.LinkLedTo, CreatedAt: created,
				})
			}, func(ctx context.Context) {
				Expect(s.EventLinks(ctx, pager.ID)).To(BeEmpty())
			}),
			Entry("CreateTimeline", func(ctx context.Context) error {
				badName := incident
				badName.ID, badName.Name = uuid(30), "inc 466"
				return s.CreateTimeline(ctx, badName)
			}, func(ctx context.Context) {
				Expect(s.Timeline(ctx, uuid(30))).Error().To(MatchError(store.ErrNotFound))
			}),
			Entry("UpdateTimeline", func(ctx context.Context) error {
				blankRef := incident
				blankRef.Refs = []string{" "}
				return s.UpdateTimeline(ctx, blankRef)
			}, func(ctx context.Context) {
				Expect(s.Timeline(ctx, incident.ID)).To(Equal(incident))
			}),
			Entry("CreateOverlay", func(ctx context.Context) error {
				return s.CreateOverlay(ctx, timeline.Overlay{
					ID: uuid(30), Name: "db outage", TimelineIDs: []string{incident.ID}, CreatedAt: created,
				})
			}, func(ctx context.Context) {
				Expect(s.TimelineOverlays(ctx, incident.ID)).To(BeEmpty())
			}),
			Entry("RenameSource", func(ctx context.Context) error {
				sourced := evidence
				sourced.ID, sourced.Source = uuid(30), new("db-2/mongod")
				Expect(s.AddAnnotation(ctx, sourced, uuid(32))).To(Succeed())
				return s.RenameSource(ctx, uuid(32), "db-2 mongod")
			}, func(ctx context.Context) {
				Expect(s.Sources(ctx)).To(ConsistOf(HaveField("Name", "db-2/mongod")))
			}),
		)

		It("adds no event when an attachment's content cannot be read", func(ctx SpecContext) {
			boom := errors.New("boom")
			stepDown := pager
			stepDown.ID = uuid(20)
			unreadable := newFile(22, "trace.pdf", nil)
			unreadable.Content = iotest.ErrReader(boom)

			_, err := s.AddEvent(ctx, stepDown, uuid(9), newFile(21, "grafana.png", screenshot), unreadable)

			Expect(err).To(MatchError(boom))
			Expect(s.Event(ctx, stepDown.ID)).Error().To(MatchError(store.ErrNotFound))
			Expect(os.ReadDir(blobDir)).To(BeEmpty())
		})

		It("adds attachments to an event it updates and an annotation it adds or updates", func(ctx SpecContext) {
			Expect(s.UpdateEvent(ctx, pager, uuid(9), newFile(21, "grafana.png", screenshot))).To(Succeed())
			Expect(s.UpdateAnnotation(ctx, evidence, uuid(9), newFile(22, "trace.pdf", trace))).To(Succeed())
			comment := evidence
			comment.ID, comment.Kind = uuid(30), timeline.AnnotationComment
			onComment := timeline.AttachmentOwner{AnnotationID: new(comment.ID)}
			Expect(s.AddAnnotation(ctx, comment, uuid(9), newFile(31, "trace.pdf", trace))).To(Succeed())

			Expect(s.ListAttachments(ctx, onPager)).To(Equal([]timeline.Attachment{
				described(21, onPager, "grafana.png", "image/png", screenshot),
			}))
			Expect(s.ListAttachments(ctx, onEvidence)).To(Equal([]timeline.Attachment{
				described(22, onEvidence, "trace.pdf", "application/pdf", trace),
			}))
			Expect(s.ListAttachments(ctx, onComment)).To(Equal([]timeline.Attachment{
				described(31, onComment, "trace.pdf", "application/pdf", trace),
			}))
		})
	})

	It("lists a timeline's attachments on its events and annotations, oldest first, and no other timeline's",
		func(ctx SpecContext) {
			other := timeline.Timeline{ID: uuid(40), Name: "inc-466", Refs: []string{}, CreatedAt: created, UpdatedAt: created}
			elsewhere := pager
			elsewhere.ID, elsewhere.TimelineID = uuid(41), other.ID
			Expect(s.CreateTimeline(ctx, other)).To(Succeed())
			Expect(s.AddEvent(ctx, elsewhere, uuid(9))).Error().NotTo(HaveOccurred())
			onEvidenceFile := add(ctx, newAttachment(12, onEvidence, "stepdown.log"), []byte("election timeout\n"))
			onPagerFile := add(ctx, newAttachment(11, onPager, "grafana.png"), screenshot)
			add(ctx, newAttachment(10, timeline.AttachmentOwner{EventID: new(elsewhere.ID)}, "other.png"), screenshot)

			Expect(s.TimelineAttachments(ctx, incident.ID)).To(Equal([]timeline.Attachment{onPagerFile, onEvidenceFile}))
		})

	It("finds attachments by id prefix, in id order, up to a limit", func(ctx SpecContext) {
		first := add(ctx, newAttachment(10, onPager, "grafana.png"), screenshot)
		second := add(ctx, newAttachment(11, onPager, "trace.png"), screenshot)

		Expect(s.AttachmentsByIDPrefix(ctx, "0199a1b2", 1)).To(Equal([]timeline.Attachment{first}))
		Expect(s.AttachmentsByIDPrefix(ctx, uuid(11), 10)).To(Equal([]timeline.Attachment{second}))
		Expect(s.AttachmentsByIDPrefix(ctx, "*", 10)).Error().To(HaveOccurred())
	})

	DescribeTable("rejects listing by an owner that is not exactly one event or annotation",
		func(ctx SpecContext, owner timeline.AttachmentOwner) {
			_, err := s.ListAttachments(ctx, owner)
			Expect(err).To(MatchError(ContainSubstring("owner")))
		},
		Entry("no owner", timeline.AttachmentOwner{}),
		Entry("both owners", timeline.AttachmentOwner{EventID: new(pager.ID), AnnotationID: new(evidence.ID)}),
	)
})
