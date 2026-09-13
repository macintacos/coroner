package store_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("Open", func() {
	It("migrates a new database file and stamps coroner's application id", func(ctx SpecContext) {
		conn, err := store.Open(ctx, filepath.Join(GinkgoT().TempDir(), "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(conn.Close)

		var applicationID int64
		Expect(conn.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID)).To(Succeed())
		Expect(applicationID).To(Equal(int64(1668248174)))
	})
})
