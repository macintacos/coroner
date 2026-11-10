package cli

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli/attachment"
	"github.com/macintacos/coroner/internal/cli/clikit"
	"github.com/macintacos/coroner/internal/cli/clitest"
)

var _ = Describe("newFileWriter", func() {
	var path string

	BeforeEach(func() {
		path = filepath.Join(GinkgoT().TempDir(), "grafana-p99.png")
	})

	It("writes a file that does not exist", func() {
		Expect(attachment.NewFileWriter(clitest.OpenFile, os.Remove)(path, []byte("GIF89a"), 0o600)).To(Succeed())

		Expect(os.ReadFile(filepath.Clean(path))).To(Equal([]byte("GIF89a")))
	})

	It("refuses a file that exists, keeping its bytes", func() {
		Expect(os.WriteFile(path, []byte("kept"), 0o600)).To(Succeed())

		Expect(attachment.NewFileWriter(clitest.OpenFile, os.Remove)(path, []byte("GIF89a"), 0o600)).To(MatchError(fs.ErrExist))

		Expect(os.ReadFile(filepath.Clean(path))).To(Equal([]byte("kept")))
	})

	It("leaves no file when writing fails", func() {
		failingWrite := func(name string, flag int, perm fs.FileMode) (io.WriteCloser, error) {
			f, err := clitest.OpenFile(name, flag, perm)
			return clitest.DiskFullWriter{WriteCloser: f}, err
		}

		Expect(attachment.NewFileWriter(failingWrite, os.Remove)(path, []byte("GIF89a"), 0o600)).To(MatchError(clitest.ErrDiskFull))

		Expect(path).NotTo(BeAnExistingFile())
	})
})

var _ = It("attachment export refuses to write to stdout on a terminal, before opening the store", func(ctx SpecContext) {
	args := []string{"attachment", "export", "0199a1b2"}
	app := clitest.StorelessApp(args)
	app.OutIsTTY = true

	Expect(executeError(ctx, app, args...)).To(HaveField("Kind", clikit.KindUsage))
})
