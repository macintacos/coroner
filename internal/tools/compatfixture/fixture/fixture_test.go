package fixture_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/tools/compatfixture/fixture"
)

var _ = Describe("Write", func() {
	It("refuses a path that already exists, leaving the file byte-identical", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		s, err := store.Open(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Close()).To(Succeed())
		before, err := os.ReadFile(filepath.Clean(path))
		Expect(err).NotTo(HaveOccurred())

		err = fixture.Write(ctx, path)

		Expect(err).To(MatchError(ContainSubstring(path + " already exists")))
		Expect(os.ReadFile(filepath.Clean(path))).To(Equal(before))
	})
})
