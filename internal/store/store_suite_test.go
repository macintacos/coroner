package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

func TestStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Store Suite")
}

// openStore opens a new store in the spec's temp directory, closed when the spec ends.
func openStore(ctx context.Context) *store.Store {
	GinkgoHelper()
	s, err := store.Open(ctx, filepath.Join(GinkgoT().TempDir(), "coroner.db"))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(s.Close)
	return s
}

// uuid returns a fixed UUIDv7-shaped id that differs for every n.
func uuid(n int) string {
	return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n)
}
