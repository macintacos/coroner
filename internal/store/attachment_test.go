package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

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
		blobDir string
	)

	attachment := func(n int, owner timeline.AttachmentOwner, filename string) timeline.Attachment {
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
		added, err := s.AddAttachment(ctx, a, bytes.NewReader(content))
		Expect(err).NotTo(HaveOccurred())
		return added
	}

	BeforeEach(func(ctx SpecContext) {
		dir = GinkgoT().TempDir()
		path := filepath.Join(dir, "coroner.db")
		blobDir = config.BlobDir(path)
		var err error
		s, err = store.Open(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		Expect(s.AddEvent(ctx, pager, uuid(9))).Error().NotTo(HaveOccurred())
		Expect(s.AddAnnotation(ctx, evidence, uuid(9))).To(Succeed())
	})

	It("stores a file under its content's hash, readable only by its owner, and lists it on its event",
		func(ctx SpecContext) {
			added := add(ctx, attachment(10, onPager, "grafana.png"), screenshot)

			want := attachment(10, onPager, "grafana.png")
			want.MIME = "image/png"
			want.SHA256 = sha256Hex(screenshot)
			want.Size = int64(len(screenshot))
			Expect(added).To(Equal(want))
			Expect(s.ListAttachments(ctx, onPager)).To(Equal([]timeline.Attachment{want}))

			blob := filepath.Join(blobDir, want.SHA256)
			Expect(os.ReadFile(filepath.Clean(blob))).To(Equal(screenshot))
			Expect(permissions(blob)).To(Equal(os.FileMode(0o600)))
			Expect(permissions(blobDir)).To(Equal(os.FileMode(0o700)))
		})

	It("lists an annotation's attachments oldest first", func(ctx SpecContext) {
		later := add(ctx, attachment(11, onEvidence, "later.png"), screenshot)
		earlier := add(ctx, attachment(10, onEvidence, "earlier.png"), []byte("\x89PNG\r\n\x1a\nearlier"))

		Expect(s.ListAttachments(ctx, onEvidence)).To(Equal([]timeline.Attachment{earlier, later}))
		Expect(s.ListAttachments(ctx, onPager)).To(BeEmpty())
	})

	It("takes the MIME type from the filename's extension when the content does not reveal it",
		func(ctx SpecContext) {
			Expect(add(ctx, attachment(10, onPager, "trace.json"), []byte{0x00, 0x01, 0x02})).
				To(HaveField("MIME", "application/json"))
		})

	It("keeps one blob for identical content until garbage collection finds no attachment using it",
		func(ctx SpecContext) {
			onEvent := add(ctx, attachment(10, onPager, "grafana.png"), screenshot)
			onAnnotation := add(ctx, attachment(11, onEvidence, "same-panel.png"), screenshot)
			blob := filepath.Join(blobDir, onEvent.SHA256)
			Expect(os.ReadDir(blobDir)).To(HaveExactElements(HaveField("Name()", onEvent.SHA256)))

			Expect(s.RemoveAttachment(ctx, onEvent.ID)).To(Succeed())
			Expect(s.GarbageCollectBlobs(ctx)).To(BeEmpty())
			Expect(blob).To(BeAnExistingFile())

			Expect(s.RemoveAttachment(ctx, onAnnotation.ID)).To(Succeed())
			Expect(s.GarbageCollectBlobs(ctx)).To(Equal([]string{onEvent.SHA256}))
			Expect(blob).NotTo(BeAnExistingFile())
		})

	It("deletes an event's attachments with it, and leaves their blobs to garbage collection", func(ctx SpecContext) {
		added := add(ctx, attachment(10, onPager, "grafana.png"), screenshot)

		Expect(s.DeleteEvents(ctx, pager.ID)).To(Succeed())

		Expect(s.ListAttachments(ctx, onPager)).To(BeEmpty())
		Expect(filepath.Join(blobDir, added.SHA256)).To(BeAnExistingFile())
		Expect(s.GarbageCollectBlobs(ctx)).To(Equal([]string{added.SHA256}))
	})

	It("leaves only an orphan blob, which garbage collection deletes, when the row cannot be written",
		func(ctx SpecContext) {
			missing := timeline.AttachmentOwner{EventID: new(uuid(99))}

			_, err := s.AddAttachment(ctx, attachment(10, missing, "grafana.png"), bytes.NewReader(screenshot))

			Expect(err).To(MatchError(store.ErrNotFound))
			Expect(s.ListAttachments(ctx, missing)).To(BeEmpty())
			Expect(filepath.Join(blobDir, sha256Hex(screenshot))).To(BeAnExistingFile())
			Expect(s.GarbageCollectBlobs(ctx)).To(Equal([]string{sha256Hex(screenshot)}))
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
				add(ctx, attachment(100+n, onPager, "panel.png"), []byte{byte(n)})
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
		added := add(ctx, attachment(10, onPager, "../../escaped.png"), screenshot)

		Expect(s.ListAttachments(ctx, onPager)).To(HaveExactElements(HaveField("Filename", "../../escaped.png")))
		Expect(filepath.Join(dir, "..", "escaped.png")).NotTo(BeAnExistingFile())
		Expect(os.ReadDir(blobDir)).To(HaveExactElements(HaveField("Name()", added.SHA256)))
	})

	It("reports removing an attachment that does not exist with ErrNotFound", func(ctx SpecContext) {
		Expect(s.RemoveAttachment(ctx, uuid(99))).To(MatchError(store.ErrNotFound))
	})

	DescribeTable("rejects an owner that is not exactly one event or annotation, writing no blob",
		func(ctx SpecContext, owner timeline.AttachmentOwner) {
			_, err := s.AddAttachment(ctx, attachment(10, owner, "grafana.png"), bytes.NewReader(screenshot))
			Expect(err).To(MatchError(ContainSubstring("owner")))
			Expect(blobDir).NotTo(BeADirectory())

			_, err = s.ListAttachments(ctx, owner)
			Expect(err).To(MatchError(ContainSubstring("owner")))
		},
		Entry("no owner", timeline.AttachmentOwner{}),
		Entry("both owners", timeline.AttachmentOwner{EventID: new(pager.ID), AnnotationID: new(evidence.ID)}),
	)
})
