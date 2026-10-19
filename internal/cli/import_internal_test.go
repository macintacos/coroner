package cli

import (
	"context"
	"database/sql"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pressly/goose/v3"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
)

var _ = DescribeTable("importBlockedError",
	func(plan importPlan, wantMessage, wantHint string) {
		Expect(importBlockedError(plan)).To(And(
			HaveField("Kind", KindIDConflict), HaveField("Message", wantMessage), HaveField("Hint", wantHint),
		))
	},
	Entry("names the conflicts under --on-conflict fail",
		importPlan{blocked: blockConflicts, result: importResultJSON{Conflicts: make([]importConflict, 2)}},
		"2 records conflict with what is stored, so nothing was imported",
		"Pass --on-conflict skip to keep what is stored, or --on-conflict replace to take the import's."),
	Entry("agrees the verb with one conflict",
		importPlan{blocked: blockConflicts, result: importResultJSON{Conflicts: make([]importConflict, 1)}},
		"1 record conflicts with what is stored, so nothing was imported",
		"Pass --on-conflict skip to keep what is stored, or --on-conflict replace to take the import's."),
	Entry("names the records --on-conflict replace cannot overwrite",
		importPlan{blocked: blockUnreplaceable, unreplaceable: make([]importConflict, 1)},
		"1 record cannot be replaced, so nothing was imported: an attachment or link never changes, and an event or annotation never moves",
		"Pass --on-conflict skip to keep what is stored."),
)

var _ = Describe("invalidImportError", func() {
	It("counts records, not problems, in its header", func() {
		problems := []string{"link l1: from event e1 is unknown", "link l1: to event e2 is unknown"}

		Expect(invalidImportError(problems, invalidImportHint)).To(MatchError(HavePrefix("1 record cannot be imported:\n")))
	})
})

var _ = Describe("readImportRecords", func() {
	It("exits 2 usage for - when stdin is a terminal, without reading it", func() {
		const line = `{"type":"event"}`
		stdin := strings.NewReader(line)

		_, err := readImportRecords(jsonlSource{path: "-", stdin: stdin, stdinIsTTY: true}, invalidImportHint)

		Expect(err).To(HaveField("Kind", KindUsage))
		Expect(KindUsage.ExitCode()).To(Equal(2))
		Expect(stdin.Len()).To(Equal(len(line)))
	})
})

var _ = Describe("sidecarBlobs", func() {
	It("leaves out a sidecar whose blob is beside the database and keeps the rest", func() {
		sidecars := map[string][]byte{"stored": []byte("rs.status() before"), "new": []byte("rs.status() after")}

		blobs := sidecarBlobs(sidecars, map[string]bool{"stored": true, "new": false})

		Expect(blobs).To(HaveLen(1))
		Expect(io.ReadAll(blobs[0])).To(Equal([]byte("rs.status() after")))
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

var _ = Describe("mergeImport", func() {
	It("plans a dry run against a database below the latest schema", func(ctx SpecContext) {
		dbPath := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		createSchemaV2(ctx, dbPath)
		app := App{OpenStore: store.Open, OpenStoreReadOnly: store.OpenReadOnly}
		file, err := os.Open(filepath.Join("..", "..", "testdata", "export", "v1.2", "links.jsonl"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(file.Close)
		records, err := export.Decode(file)
		Expect(err).NotTo(HaveOccurred())
		in := mergeInput{records: records, timelines: timelineInput{rule: timelineRule{guard: true}}}

		plan, err := mergeImport(ctx, app, dbPath, in, true)

		Expect(err).NotTo(HaveOccurred())
		Expect(plan.result.Links.Inserted).To(BeNumerically(">", 0))
	})
})

// createSchemaV2 writes a database at dbPath migrated only through schema 2.
func createSchemaV2(ctx context.Context, dbPath string) {
	GinkgoHelper()
	all := os.DirFS(filepath.Join("..", "store", "migrations"))
	migrations := fstest.MapFS{}
	for _, name := range []string{"00001_initial_schema.sql", "00002_attachments.sql"} {
		data, err := fs.ReadFile(all, name)
		Expect(err).NotTo(HaveOccurred())
		migrations[name] = &fstest.MapFile{Data: data}
	}
	db, err := sql.Open("sqlite", dbPath)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(db.Close)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations)
	Expect(err).NotTo(HaveOccurred())
	_, err = provider.Up(ctx)
	Expect(err).NotTo(HaveOccurred())
}
