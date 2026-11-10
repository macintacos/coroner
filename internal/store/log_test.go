package store_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Diagnostic lines", func() {
	It("logs a database it creates, its schema version and every migration it applies", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		var logs bytes.Buffer
		s, err := store.Open(ctx, path, capture(&logs))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		schema, err := s.Schema(ctx)
		Expect(err).NotTo(HaveOccurred())
		latest := schema.Latest

		Expect(logged(&logs, "database opened")).To(HaveExactElements(SatisfyAll(
			HaveKeyWithValue("level", "DEBUG"), HaveKeyWithValue("path", path), HaveKeyWithValue("created", true),
			HaveKeyWithValue("schema_version", BeNumerically("==", latest)))))
		var applied []any
		for v := int64(1); v <= latest; v++ {
			applied = append(applied, SatisfyAll(HaveKeyWithValue("level", "INFO"), HaveKeyWithValue("schema_version", BeNumerically("==", v))))
		}
		Expect(logged(&logs, "migration applied")).To(HaveExactElements(applied...))
		Expect(logged(&logs, "database backed up")).To(BeEmpty())
	})

	It("logs a database that already exists, with no migration", func(ctx SpecContext) {
		var logs bytes.Buffer
		s, err := store.Open(ctx, newDatabase(), capture(&logs))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		schema, err := s.Schema(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(logged(&logs, "database opened")).To(HaveExactElements(SatisfyAll(
			HaveKeyWithValue("created", false), HaveKeyWithValue("schema_version", BeNumerically("==", schema.Latest)))))
		Expect(logged(&logs, "migration applied")).To(BeEmpty())
	})

	It("logs a database it opens read-only below the latest schema, and migrates nothing", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		old, err := store.OpenWithMigrations(ctx, path, schemaUpTo(1))
		Expect(err).NotTo(HaveOccurred())
		Expect(old.Close()).To(Succeed())

		var logs bytes.Buffer
		s, err := store.OpenReadOnly(ctx, path, capture(&logs))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		Expect(logged(&logs, "database opened")).To(HaveExactElements(SatisfyAll(
			HaveKeyWithValue("created", false), HaveKeyWithValue("schema_version", BeNumerically("==", 1)))))
		Expect(logged(&logs, "migration applied")).To(BeEmpty())
	})

	It("logs a blob the first time it is stored, and each blob garbage collection deletes", func(ctx SpecContext) {
		var logs bytes.Buffer
		s, err := store.Open(ctx, newDatabase(), capture(&logs))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		Expect(s.CreateTimeline(ctx, changeIncident())).To(Succeed())
		event := changeEvent(2)
		Expect(s.AddEvent(ctx, event, uuid(9))).Error().NotTo(HaveOccurred())

		content := []byte("election timeout after 10000ms")
		owner := timeline.AttachmentOwner{EventID: new(event.ID)}
		first, err := attach(ctx, s, timeline.Attachment{ID: uuid(10), Owner: owner, Filename: "mongod.log"}, bytes.NewReader(content))
		Expect(err).NotTo(HaveOccurred())
		_, err = attach(ctx, s, timeline.Attachment{ID: uuid(11), Owner: owner, Filename: "mongod.log"}, bytes.NewReader(content))
		Expect(err).NotTo(HaveOccurred())
		sha := first.SHA256

		blob := SatisfyAll(HaveKeyWithValue("sha256", sha), HaveKeyWithValue("size", BeNumerically("==", len(content))))
		Expect(logged(&logs, "blob stored")).To(HaveExactElements(SatisfyAll(HaveKeyWithValue("level", "DEBUG"), blob)))

		Expect(s.DeleteAttachments(ctx, uuid(10), uuid(11))).To(Succeed())
		Expect(s.GarbageCollectBlobs(ctx)).To(Equal([]string{sha}))

		Expect(logged(&logs, "blob deleted")).To(HaveExactElements(SatisfyAll(HaveKeyWithValue("level", "INFO"), blob)))
		Expect(logged(&logs, "blobs collected")).To(HaveExactElements(SatisfyAll(
			HaveKeyWithValue("level", "DEBUG"), HaveKeyWithValue("count", BeNumerically("==", 1)))))
		Expect(logs.String()).NotTo(ContainSubstring("mongod.log"))
	})

	failed := errors.New("fn failed")

	DescribeTable("logs how each transaction ended and how long it took",
		func(ctx SpecContext, run func(context.Context, *store.Store) error, wantErr error, msg string, keys types.GomegaMatcher) {
			var logs bytes.Buffer
			s, err := store.Open(ctx, newDatabase(), capture(&logs), ticking())
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(s.Close)

			Expect(errors.Is(run(ctx, s), wantErr)).To(BeTrue())

			Expect(logged(&logs, msg)).To(HaveExactElements(SatisfyAll(
				HaveKeyWithValue("level", "DEBUG"), HaveKeyWithValue("duration", BeNumerically("==", 1_000_000)), keys)))
		},
		Entry("a change that commits", func(ctx context.Context, s *store.Store) error {
			_, err := s.Change(ctx, store.ChangeOptions{}, func(*store.Tx) error { return nil })
			return err
		}, nil, "transaction committed", SatisfyAll(HaveKeyWithValue("read_only", false), Not(HaveKey("dry_run")))),
		Entry("a change whose function fails", func(ctx context.Context, s *store.Store) error {
			_, err := s.Change(ctx, store.ChangeOptions{}, func(*store.Tx) error { return failed })
			return err
		}, failed, "transaction rolled back", SatisfyAll(HaveKeyWithValue("read_only", false), HaveKeyWithValue("dry_run", false))),
		Entry("a dry run", func(ctx context.Context, s *store.Store) error {
			_, err := s.Change(ctx, store.ChangeOptions{Mode: store.Rollback}, func(*store.Tx) error { return nil })
			return err
		}, nil, "transaction rolled back", SatisfyAll(HaveKeyWithValue("read_only", false), HaveKeyWithValue("dry_run", true))),
		Entry("a read", func(ctx context.Context, s *store.Store) error {
			return s.Read(ctx, func(*store.Tx) error { return nil })
		}, nil, "transaction committed", SatisfyAll(HaveKeyWithValue("read_only", true), Not(HaveKey("dry_run")))),
	)
})

// ticking is a clock that advances a millisecond each time it is read.
func ticking() store.Option {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	return store.WithClock(func() time.Time {
		now = now.Add(time.Millisecond)
		return now
	})
}
