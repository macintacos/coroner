package store_test

import (
	"bytes"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
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
})
