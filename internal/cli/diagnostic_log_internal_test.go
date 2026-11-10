package cli

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/macintacos/coroner/internal/config"
)

func testDiagnosticLog() *diagnosticLog {
	return &diagnosticLog{
		stat:   os.Stat,
		open:   openFile,
		rename: os.Rename,
		limit:  64,
		now:    func() time.Time { return time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC) },
	}
}

func readLines(path string) []string {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred())
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

type failingFile struct{ failingWriter }

func (failingFile) Close() error { return errDiskFull }

var _ = Describe("diagnostic log", func() {
	var dir, db, logPath string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		db = filepath.Join(dir, "coroner.db")
		logPath = config.DiagnosticLogPath(db)
	})

	createDB := func() {
		GinkgoHelper()
		Expect(os.WriteFile(db, nil, 0o600)).To(Succeed())
	}

	It("holds lines until a database that exists is opened, then writes them in order and appends later ones", func() {
		createDB()
		d := testDiagnosticLog()
		_, _ = d.Write([]byte("one\n"))
		_, _ = d.Write([]byte("two\n"))
		Expect(logPath).NotTo(BeAnExistingFile())

		d.opened(filepath.Join(dir, "missing.db"), fs.ErrNotExist)
		d.opened(db, nil)
		_, _ = d.Write([]byte("three\n"))
		d.close()

		Expect(readLines(logPath)).To(Equal([]string{"one", "two", "three"}))
	})

	It("writes nothing and creates nothing when only missing databases are opened", func() {
		d := testDiagnosticLog()
		_, _ = d.Write([]byte("one\n"))
		d.opened(filepath.Join(dir, "sub", "missing.db"), fs.ErrNotExist)
		d.close()

		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("creates the log readable by its owner only", func() {
		createDB()
		d := testDiagnosticLog()
		d.opened(db, nil)
		d.close()

		info, err := os.Stat(logPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(fs.FileMode(0o600)))
	})

	It("moves a log over the limit to <log>.1, replacing an older one, and starts a new log", func() {
		createDB()
		Expect(os.WriteFile(logPath, []byte(strings.Repeat("x", 65)+"\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(logPath+".1", []byte("older\n"), 0o600)).To(Succeed())
		d := testDiagnosticLog()
		_, _ = d.Write([]byte("new\n"))
		d.opened(db, nil)
		d.close()

		Expect(readLines(logPath)).To(Equal([]string{"new"}))
		Expect(readLines(logPath + ".1")).To(Equal([]string{strings.Repeat("x", 65)}))
		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(3)) // the database, the log and <log>.1
	})

	It("appends to a log at the limit", func() {
		createDB()
		Expect(os.WriteFile(logPath, []byte(strings.Repeat("x", 63)+"\n"), 0o600)).To(Succeed())
		d := testDiagnosticLog()
		_, _ = d.Write([]byte("new\n"))
		d.opened(db, nil)
		d.close()

		Expect(readLines(logPath)).To(Equal([]string{strings.Repeat("x", 63), "new"}))
		Expect(logPath + ".1").NotTo(BeAnExistingFile())
	})

	DescribeTable("never fails when the file system does",
		func(breakLog func(d *diagnosticLog)) {
			createDB()
			overLimit := strings.Repeat("x", 65) + "\n"
			Expect(os.WriteFile(logPath, []byte(overLimit), 0o600)).To(Succeed())
			d := testDiagnosticLog()
			breakLog(d)

			Expect(func() {
				n, err := d.Write([]byte("one\n"))
				Expect(n).To(Equal(4))
				Expect(err).NotTo(HaveOccurred())
				d.opened(db, nil)
				n, err = d.Write([]byte("two\n"))
				Expect(n).To(Equal(4))
				Expect(err).NotTo(HaveOccurred())
				d.close()
			}).NotTo(Panic())
			entries, err := os.ReadDir(dir)
			Expect(err).NotTo(HaveOccurred())
			for _, entry := range entries {
				Expect(entry.Name()).To(BeElementOf("coroner.db", "coroner.log", "coroner.log.1"))
				Expect(readLines(filepath.Join(dir, entry.Name()))).NotTo(ContainElement(BeElementOf("one", "two")), entry.Name())
			}
		},
		Entry("stat of the database", func(d *diagnosticLog) {
			d.stat = func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
		}),
		Entry("stat of the log", func(d *diagnosticLog) {
			d.stat = func(name string) (fs.FileInfo, error) {
				if strings.HasSuffix(name, ".log") {
					return nil, fs.ErrPermission
				}
				return os.Stat(name)
			}
		}),
		Entry("open", func(d *diagnosticLog) {
			d.open = func(string, int, fs.FileMode) (io.WriteCloser, error) { return nil, fs.ErrPermission }
		}),
		Entry("rename", func(d *diagnosticLog) {
			d.rename = func(string, string) error { return fs.ErrPermission }
		}),
		Entry("the file's write", func(d *diagnosticLog) {
			d.open = func(string, int, fs.FileMode) (io.WriteCloser, error) { return failingFile{}, nil }
		}),
	)

	It("drops writes after close", func() {
		createDB()
		d := testDiagnosticLog()
		d.opened(db, nil)
		_, _ = d.Write([]byte("one\n"))
		d.close()
		_, _ = d.Write([]byte("two\n"))

		Expect(readLines(logPath)).To(Equal([]string{"one"}))
	})

	It("keeps every line whole when two commands append to one log at once", func() {
		createDB()
		var wg sync.WaitGroup
		for range 2 {
			d := testDiagnosticLog()
			// The first writer may be past the 64-byte limit when the second opens, which would rotate it.
			d.limit = 1 << 30
			d.opened(db, nil)
			logger := d.logger(io.Discard)
			wg.Go(func() {
				for i := range 300 {
					logger.Info("line", "i", i, "padding", strings.Repeat("p", 200))
				}
				d.close()
			})
		}
		wg.Wait()

		lines := readLines(logPath)
		Expect(lines).To(HaveLen(600))
		for _, line := range lines {
			Expect(json.Valid([]byte(line))).To(BeTrue(), line)
		}
	})

	DescribeTable("tells the command a command line runs, and whether it stays out of the log",
		func(ctx SpecContext, args []string, wantPath string, wantOff bool) {
			path, off := commandOf(NewRoot(ctx, App{}), args)

			Expect(path).To(Equal(wantPath))
			Expect(off).To(Equal(wantOff))
		},
		Entry(nil, []string{"version"}, "coroner version", true),
		Entry(nil, []string{"agent", "skill"}, "coroner agent skill", true),
		Entry(nil, []string{"completion", "zsh"}, "coroner completion zsh", true),
		Entry(nil, []string{"__complete", "event", "list", "-t", ""}, "coroner __complete", true),
		Entry(nil, []string{"__internal"}, "coroner __internal", true),
		Entry(nil, []string{"timeline", "list"}, "coroner timeline list", false),
		Entry(nil, []string{"help"}, "coroner help", false),
	)

	It("lets only agent skill, completion, version and __internal stay out of the log", func(ctx SpecContext) {
		var off []string
		walkCommands(NewRoot(ctx, App{}), func(cmd *cobra.Command) {
			if cmd.Annotations[diagnosticLogAnnotation] == "off" {
				off = append(off, cmd.CommandPath())
			}
		})

		Expect(off).To(ConsistOf("coroner agent skill", "coroner completion", "coroner version", "coroner __internal"))
	})
})
