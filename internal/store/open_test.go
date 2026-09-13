package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing/fstest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/store/sqlcgen"
)

var _ = Describe("Open", func() {
	It("migrates a new database file at a path containing a space, ?, # and %, and stamps coroner's application id",
		func(ctx SpecContext) {
			path := filepath.Join(GinkgoT().TempDir(), "odd ?#% dir", "coroner ?#%25.db")

			s, err := store.Open(ctx, path)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(s.Close)

			Expect(path).To(BeAnExistingFile())
			var applicationID int64
			Expect(s.DB().QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID)).To(Succeed())
			Expect(applicationID).To(Equal(int64(1668248174)))
		})

	It("puts the database in WAL mode, so a reader never waits for a writer", func(ctx SpecContext) {
		db, err := sql.Open("sqlite", migratedDatabase(ctx))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)
		var mode string
		Expect(db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode)).To(Succeed())
		Expect(mode).To(Equal("wal"))
	})

	It("creates a missing parent directory that only its owner can use", func(ctx SpecContext) {
		dir := filepath.Join(GinkgoT().TempDir(), "coroner")

		s, err := store.Open(ctx, filepath.Join(dir, "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		info, err := os.Stat(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
	})

	DescribeTable("refuses a SQLite file coroner did not create, leaving it byte-identical",
		func(ctx SpecContext, statement string) {
			path := filepath.Join(GinkgoT().TempDir(), "other.db")
			execSQLite(ctx, path, statement)
			before, err := os.ReadFile(filepath.Clean(path))
			Expect(err).NotTo(HaveOccurred())

			_, err = store.Open(ctx, path)

			Expect(err).To(MatchError(store.ErrForeignDatabase))
			Expect(os.ReadFile(filepath.Clean(path))).To(Equal(before))
		},
		Entry("with an unrelated table", "CREATE TABLE contact (name TEXT)"),
		Entry("with another application id", "PRAGMA application_id = 42"),
		Entry("with another program's goose_db_version table", `
			CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY AUTOINCREMENT, version_id INTEGER NOT NULL,
				is_applied INTEGER NOT NULL, tstamp TIMESTAMP DEFAULT (datetime('now')));
			INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, 1), (1, 1);
			CREATE TABLE users (name TEXT);`),
	)

	It("refuses a database a newer coroner migrated, leaving it byte-identical with nothing beside it",
		func(ctx SpecContext) {
			path := migratedDatabase(ctx)
			bumpSchemaVersion(ctx, path)
			before, err := os.ReadFile(filepath.Clean(path))
			Expect(err).NotTo(HaveOccurred())

			_, err = store.Open(ctx, path)

			Expect(err).To(MatchError(store.ErrDatabaseTooNew))
			Expect(os.ReadFile(filepath.Clean(path))).To(Equal(before))
			Expect(os.ReadDir(filepath.Dir(path))).To(HaveExactElements(HaveField("Name()", "coroner.db")))
		})

	It("finishes a first migration that failed after creating the version table", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		failing := fstest.MapFS{"00001_initial_schema.sql": {
			Data: []byte("-- +goose Up\nPRAGMA application_id = 1668248174;\nSELECT * FROM missing;\n"),
		}}
		_, err := store.OpenWithMigrations(ctx, path, failing)
		Expect(err).To(HaveOccurred())

		s, err := store.Open(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		Expect(querySQLite(ctx, path, "SELECT count(*) FROM timeline")).To(BeZero())
	})

	It("reports a database another connection holds locked as ErrBusy", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		db, err := sql.Open("sqlite", path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)
		conn, err := db.Conn(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(conn.Close)
		Expect(conn.ExecContext(ctx, "BEGIN EXCLUSIVE")).Error().NotTo(HaveOccurred())

		_, err = store.Open(ctx, path)

		Expect(err).To(MatchError(store.ErrBusy))
	}, SpecTimeout(time.Minute))

	It("takes no backup of a new database", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "coroner.db")

		s, err := store.Open(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		Expect(filepath.Glob(path + ".pre-v*.bak")).To(BeEmpty())
	})

	Describe("upgrading a database that holds data", func() {
		const laterTables = "SELECT count(*) FROM sqlite_schema WHERE name = 'later'"

		var path, backup string
		var v2 fstest.MapFS

		BeforeEach(func(ctx SpecContext) {
			initial, err := os.ReadFile("migrations/00001_initial_schema.sql")
			Expect(err).NotTo(HaveOccurred())
			v1 := fstest.MapFS{"00001_initial_schema.sql": {Data: initial}}
			v2 = fstest.MapFS{
				"00001_initial_schema.sql": {Data: initial},
				"00002_later.sql":          {Data: []byte("-- +goose Up\nCREATE TABLE later (x TEXT) STRICT;\n")},
			}

			path = filepath.Join(GinkgoT().TempDir(), "coroner.db")
			backup = path + ".pre-v2.bak"
			s, err := store.OpenWithMigrations(ctx, path, v1)
			Expect(err).NotTo(HaveOccurred())
			_, err = sqlcgen.New(s.DB()).CreateTimeline(ctx, sqlcgen.CreateTimelineParams{
				ID:        "0199a1b2-0000-7000-8000-000000000001",
				Name:      "inc-465",
				CreatedAt: "2026-09-13T09:00:00.000000000Z",
				UpdatedAt: "2026-09-13T09:00:00.000000000Z",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Close()).To(Succeed())
		})

		It("first copies the database as it was to <db>.pre-v<N>.bak, N being the schema it upgrades to",
			func(ctx SpecContext) {
				s, err := store.OpenWithMigrations(ctx, path, v2)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(s.Close)

				Expect(querySQLite(ctx, backup, "SELECT count(*) FROM timeline")).To(Equal(int64(1)))
				Expect(querySQLite(ctx, backup, laterTables)).To(BeZero())
				Expect(querySQLite(ctx, path, laterTables)).To(Equal(int64(1)))
			})

		It("refuses to migrate, naming the backup, when that backup already exists", func(ctx SpecContext) {
			Expect(os.WriteFile(backup, []byte("an earlier backup"), 0o600)).To(Succeed())

			_, err := store.OpenWithMigrations(ctx, path, v2)

			Expect(err).To(And(MatchError(store.ErrBackupExists), MatchError(ContainSubstring(backup))))
			Expect(os.ReadFile(filepath.Clean(backup))).To(Equal([]byte("an earlier backup")))
			Expect(querySQLite(ctx, path, laterTables)).To(BeZero())
		})
	})
})

var _ = Describe("OpenReadOnly", func() {
	It("reports a missing path as not existing, creating neither it nor its directory", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "missing", "coroner.db")

		_, err := store.OpenReadOnly(ctx, path)

		Expect(err).To(MatchError(fs.ErrNotExist))
		Expect(filepath.Dir(path)).NotTo(BeAnExistingFile())
	})

	It("opens a coroner database at the latest schema, leaving it byte-identical", func(ctx SpecContext) {
		path := migratedDatabase(ctx)
		migrated := querySQLite(ctx, path, "SELECT max(version_id) FROM goose_db_version")
		before, err := os.ReadFile(filepath.Clean(path))
		Expect(err).NotTo(HaveOccurred())

		s, err := store.OpenReadOnly(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		schema, err := s.Schema(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Close()).To(Succeed())

		Expect(schema).To(Equal(store.Schema{Version: migrated, Latest: migrated}))
		Expect(os.ReadFile(filepath.Clean(path))).To(Equal(before))
	})

	It("gives a connection that refuses writes", func(ctx SpecContext) {
		s, err := store.OpenReadOnly(ctx, migratedDatabase(ctx))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		Expect(s.DB().ExecContext(ctx, "CREATE TABLE scratch (x TEXT)")).Error().
			To(MatchError(ContainSubstring("readonly")))
	})

	It("refuses a database a newer coroner migrated, leaving it byte-identical", func(ctx SpecContext) {
		path := migratedDatabase(ctx)
		bumpSchemaVersion(ctx, path)
		before, err := os.ReadFile(filepath.Clean(path))
		Expect(err).NotTo(HaveOccurred())

		_, err = store.OpenReadOnly(ctx, path)

		Expect(err).To(MatchError(store.ErrDatabaseTooNew))
		Expect(os.ReadFile(filepath.Clean(path))).To(Equal(before))
	})

	It("refuses a SQLite file coroner did not create", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "other.db")
		execSQLite(ctx, path, "CREATE TABLE contact (name TEXT)")

		_, err := store.OpenReadOnly(ctx, path)

		Expect(err).To(MatchError(store.ErrForeignDatabase))
	})

	It("opens an empty file at schema version 0, below the latest", func(ctx SpecContext) {
		latest := querySQLite(ctx, migratedDatabase(ctx), "SELECT max(version_id) FROM goose_db_version")
		path := filepath.Join(GinkgoT().TempDir(), "empty.db")
		Expect(os.WriteFile(path, nil, 0o600)).To(Succeed())

		s, err := store.OpenReadOnly(ctx, path)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		Expect(s.Schema(ctx)).To(Equal(store.Schema{Version: 0, Latest: latest}))
	})
})

// migratedDatabase returns the path of a new database Open migrated to the latest schema.
func migratedDatabase(ctx SpecContext) string {
	GinkgoHelper()
	path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
	s, err := store.Open(ctx, path)
	Expect(err).NotTo(HaveOccurred())
	Expect(s.Close()).To(Succeed())
	return path
}

// bumpSchemaVersion records a migration one past the highest the database at path has
// applied, which for a migratedDatabase is past every embedded one, as a newer coroner would.
func bumpSchemaVersion(ctx context.Context, path string) {
	GinkgoHelper()
	execSQLite(ctx, path,
		"INSERT INTO goose_db_version (version_id, is_applied) SELECT max(version_id) + 1, 1 FROM goose_db_version")
}

// execSQLite runs statement on the SQLite file at path without going through the store.
func execSQLite(ctx context.Context, path, statement string) {
	GinkgoHelper()
	db, err := sql.Open("sqlite", path)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(db.Close)
	Expect(db.ExecContext(ctx, statement)).Error().NotTo(HaveOccurred())
	Expect(db.Close()).To(Succeed())
}

// querySQLite returns the single integer query selects from the SQLite file at path,
// without going through the store.
func querySQLite(ctx context.Context, path, query string) int64 {
	GinkgoHelper()
	db, err := sql.Open("sqlite", path)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(db.Close)

	var n int64
	Expect(db.QueryRowContext(ctx, query).Scan(&n)).To(Succeed())
	return n
}
