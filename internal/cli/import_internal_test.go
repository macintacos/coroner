package cli

import (
	"io/fs"
	"strings"
	"testing/fstest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
)

var _ = Describe("readImportRecords", func() {
	It("exits 2 usage for - when stdin is a terminal, without reading it", func() {
		const line = `{"type":"event"}`
		stdin := strings.NewReader(line)

		_, err := readImportRecords("-", nil, stdin, true)

		Expect(err).To(HaveField("Kind", KindUsage))
		Expect(KindUsage.ExitCode()).To(Equal(2))
		Expect(stdin.Len()).To(Equal(len(line)))
	})
})

var _ = Describe("readSidecars", func() {
	var (
		fsys  fstest.MapFS
		reads int
	)

	readDir := func(name string) ([]fs.DirEntry, error) { return fs.ReadDir(fsys, name) }
	readFile := func(name string) ([]byte, error) {
		reads++
		return fs.ReadFile(fsys, name)
	}

	BeforeEach(func() {
		fsys, reads = fstest.MapFS{}, 0
	})

	It("finds a file by its name up to the first ., and reads a file two attachments share once", func() {
		fsys["inc/attachments/abc.log.gz"] = &fstest.MapFile{Data: []byte("stepdown")}
		records := []export.Record{
			export.Attachment{ID: "a1", SHA256: "abc", MIME: "text/plain"},
			export.Attachment{ID: "a2", SHA256: "abc", MIME: "text/plain"},
			export.Attachment{ID: "a3", SHA256: "def", MIME: "text/plain"},
		}

		sidecars, err := readSidecars("inc/inc-4821.jsonl", records, readDir, readFile)

		Expect(err).NotTo(HaveOccurred())
		Expect(sidecars).To(Equal(map[string][]byte{"abc": []byte("stepdown")}))
		Expect(reads).To(Equal(1))
	})

	It("finds no files when no attachments/ directory is beside the export", func() {
		records := []export.Record{export.Attachment{ID: "a1", SHA256: "abc"}}

		sidecars, err := readSidecars("inc/inc-4821.jsonl", records, readDir, readFile)

		Expect(err).NotTo(HaveOccurred())
		Expect(sidecars).To(BeEmpty())
	})
})
