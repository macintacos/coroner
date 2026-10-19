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

// openPropertyStore opens one store shared by every run of a rapid property, closed when t ends,
// and returns an id generator, so ids never repeat across runs.
func openPropertyStore(t *testing.T) (*store.Store, func() string) {
	t.Helper()
	s, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "coroner.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := s.Close()
		if err != nil {
			t.Error(err)
		}
	})

	n := 0
	return s, func() string {
		n++
		return uuid(n)
	}
}

// uuid returns a fixed UUIDv7-shaped id that differs for every n.
func uuid(n int) string {
	return fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", n)
}
