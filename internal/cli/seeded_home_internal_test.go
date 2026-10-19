package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

// migrated is a database store.Open migrated to the latest schema, built once per test binary.
var migrated = sync.OnceValues(func() ([]byte, error) { //nolint:gochecknoglobals // built once, never written after
	dir, err := os.MkdirTemp("", "coroner-migrated")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "coroner.db")
	s, err := store.Open(context.Background(), path)
	if err != nil {
		return nil, err
	}
	err = s.Close()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Clean(path))
})

// writeMigratedDatabase writes a database at the latest schema to path, as the first write
// command there would, so a spec that is not about creating the database skips migrating it.
func writeMigratedDatabase(path string) {
	GinkgoHelper()
	data, err := migrated()
	Expect(errors.Join(err, os.WriteFile(path, data, 0o600))).To(Succeed())
}

// seededHome is the home directory a container's seed built, kept in memory so each later spec
// starts from a copy of it instead of running the seed again.
type seededHome struct {
	taken bool
	files []seededFile
}

type seededFile struct {
	path string
	mode fs.FileMode
	data []byte
}

// into runs seed in home the first time and remembers what it left there; every later time it
// writes that back into home, with the same modes. The seed must leave no state outside home
// that its caller does not capture itself.
func (h *seededHome) into(home string, seed func()) {
	GinkgoHelper()
	if !h.taken {
		seed()
		h.files = readHome(home)
		h.taken = true
		return
	}
	for _, f := range h.files {
		path := filepath.Join(home, f.path)
		if f.mode.IsDir() {
			Expect(os.MkdirAll(path, f.mode.Perm())).To(Succeed())
			Expect(os.Chmod(path, f.mode.Perm())).To(Succeed())
			continue
		}
		Expect(os.WriteFile(path, f.data, f.mode.Perm())).To(Succeed())
	}
}

func readHome(home string) []seededFile {
	GinkgoHelper()
	var files []seededFile
	fsys := os.DirFS(home)
	Expect(fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		f := seededFile{path: filepath.FromSlash(path), mode: info.Mode()}
		if !d.IsDir() {
			Expect(info.Mode().IsRegular()).To(BeTrue(), path)
			f.data, err = fs.ReadFile(fsys, path)
		}
		files = append(files, f)
		return err
	})).To(Succeed())
	return files
}
