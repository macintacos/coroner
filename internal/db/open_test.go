package db_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/db"
)

var _ = Describe("Open", func() {
	It("migrates and round-trips an author", func(ctx SpecContext) {
		conn, err := db.Open(ctx, filepath.Join(GinkgoT().TempDir(), "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(conn.Close)

		queries := db.New(conn)
		created, err := queries.CreateAuthor(ctx, db.CreateAuthorParams{Name: "Brian Kernighan"})
		Expect(err).NotTo(HaveOccurred())
		Expect(queries.GetAuthor(ctx, created.ID)).To(Equal(created))
	})
})
