package cli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("newFileWriter", func() {
	var path string

	BeforeEach(func() {
		path = filepath.Join(GinkgoT().TempDir(), "grafana-p99.png")
	})

	It("writes a file that does not exist", func() {
		Expect(newFileWriter(openFile, os.Remove)(path, []byte("GIF89a"), 0o600)).To(Succeed())

		Expect(os.ReadFile(filepath.Clean(path))).To(Equal([]byte("GIF89a")))
	})

	It("refuses a file that exists, keeping its bytes", func() {
		Expect(os.WriteFile(path, []byte("kept"), 0o600)).To(Succeed())

		Expect(newFileWriter(openFile, os.Remove)(path, []byte("GIF89a"), 0o600)).To(MatchError(fs.ErrExist))

		Expect(os.ReadFile(filepath.Clean(path))).To(Equal([]byte("kept")))
	})

	It("leaves no file when writing fails", func() {
		failingWrite := func(name string, flag int, perm fs.FileMode) (io.WriteCloser, error) {
			f, err := openFile(name, flag, perm)
			return failingWriter{f}, err
		}

		Expect(newFileWriter(failingWrite, os.Remove)(path, []byte("GIF89a"), 0o600)).To(MatchError(errDiskFull))

		Expect(path).NotTo(BeAnExistingFile())
	})
})

var _ = It("attachment export refuses to write to stdout on a terminal, before opening the store", func(ctx SpecContext) {
	args := []string{"attachment", "export", "0199a1b2"}
	root := NewRoot(ctx, App{
		Args: args, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, OutIsTTY: true,
		LookupEnv: func(key string) (string, bool) {
			value, ok := map[string]string{"HOME": "/nonexistent", "CORONER_DB": "/nonexistent/coroner.db"}[key]
			return value, ok
		},
		ReadFile: func(string) ([]byte, error) { return nil, fs.ErrNotExist },
		Now:      testNow,
		NewID:    func() (uuid.UUID, error) { return uuid.UUID{}, nil },
		OpenStore: func(context.Context, string) (*store.Store, error) {
			Fail("attachment export opened the store")
			return nil, errors.New("unreachable")
		},
	})
	root.SetArgs(args)

	_, err := root.ExecuteContextC(ctx)

	Expect(err).To(HaveField("Kind", KindUsage))
})

var errDiskFull = errors.New("disk full")

// failingWriter is a file whose every write fails with errDiskFull.
type failingWriter struct{ io.WriteCloser }

func (failingWriter) Write([]byte) (int, error) { return 0, errDiskFull }

func openFile(name string, flag int, perm fs.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(filepath.Clean(name), flag, perm)
}
