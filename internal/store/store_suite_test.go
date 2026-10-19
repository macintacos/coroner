package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

func TestStore(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Store Suite")
}

// migrated is a database Open migrated to the latest schema once per suite run, which
// newDatabase copies so a spec not about migrating skips it.
var migrated []byte //nolint:gochecknoglobals // written once in BeforeSuite, read-only after

var _ = BeforeSuite(func(ctx SpecContext) {
	path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
	s, err := store.Open(ctx, path)
	Expect(err).NotTo(HaveOccurred())
	Expect(s.Close()).To(Succeed())
	migrated, err = os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred())
})

// newDatabase returns the path of a copy of the migrated database in the spec's temp directory.
func newDatabase() string {
	GinkgoHelper()
	path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
	Expect(os.WriteFile(path, migrated, 0o600)).To(Succeed())
	return path
}

// openStore opens a new store in the spec's temp directory, closed when the spec ends.
func openStore(ctx context.Context) *store.Store {
	GinkgoHelper()
	s, err := store.Open(ctx, newDatabase())
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
