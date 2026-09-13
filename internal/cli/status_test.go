package cli_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("status", func() {
	type statusJSON struct {
		DBPath        string `json:"db_path"`
		DBExists      bool   `json:"db_exists"`
		SchemaVersion *int64 `json:"schema_version"`
		Timeline      *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"timeline"`
		SelectedBy *string `json:"selected_by"`
	}

	var (
		out, errOut bytes.Buffer
		dbPath      string
		env         map[string]string
		ids         int
	)

	run := func(ctx SpecContext, args ...string) int {
		out.Reset()
		errOut.Reset()
		return cli.Run(ctx, cli.App{
			Args: append([]string{"--db", dbPath}, args...),
			Out:  &out,
			Err:  &errOut,
			LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			},
			ReadFile: readNoFile,
			Now:      func() time.Time { return time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC) },
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			},
			OpenStore:         store.Open,
			OpenStoreReadOnly: store.OpenReadOnly,
		})
	}

	status := func(ctx SpecContext, args ...string) statusJSON {
		GinkgoHelper()
		Expect(run(ctx, append([]string{"status", "--json"}, args...)...)).To(Equal(0), errOut.String())
		var got statusJSON
		Expect(json.Unmarshal(out.Bytes(), &got)).To(Succeed())
		return got
	}

	BeforeEach(func() {
		dbPath = filepath.Join(GinkgoT().TempDir(), "coroner.db")
		env = map[string]string{"HOME": GinkgoT().TempDir()}
		ids = 0
	})

	It("reports a database that does not exist without creating it or its directory", func(ctx SpecContext) {
		dbPath = filepath.Join(GinkgoT().TempDir(), "missing", "coroner.db")

		Expect(run(ctx, "status", "--json")).To(Equal(0))

		Expect(out.String()).To(MatchJSON(fmt.Sprintf(
			`{"db_path": %q, "db_exists": false, "schema_version": null, "timeline": null, "selected_by": null}`, dbPath)))
		Expect(filepath.Dir(dbPath)).NotTo(BeAnExistingFile())
	})

	Describe("with timelines", func() {
		BeforeEach(func(ctx SpecContext) {
			for _, name := range []string{"first", "second", "third"} {
				Expect(run(ctx, "timeline", "create", name)).To(Equal(0))
			}
		})

		It("reports the database's path and schema version, and no timeline when none is selected",
			func(ctx SpecContext) {
				got := status(ctx)

				Expect(got.DBPath).To(Equal(dbPath))
				Expect(got.DBExists).To(BeTrue())
				Expect(got.SchemaVersion).To(HaveValue(BeNumerically(">", 0)))
				Expect(got.Timeline).To(BeNil())
				Expect(got.SelectedBy).To(BeNil())
			})

		DescribeTable("reports the timeline selected and the layer that selected it",
			func(ctx SpecContext, envTimeline string, args []string, wantName, wantBy string) {
				Expect(run(ctx, "timeline", "use", "first")).To(Equal(0))
				if envTimeline != "" {
					env["CORONER_TIMELINE"] = envTimeline
				}

				got := status(ctx, args...)

				Expect(got.Timeline).NotTo(BeNil())
				Expect(got.Timeline.Name).To(Equal(wantName))
				Expect(got.Timeline.ID).To(HaveLen(36))
				Expect(got.SelectedBy).To(HaveValue(Equal(wantBy)))
			},
			Entry("timeline use", "", nil, "first", "use"),
			Entry("CORONER_TIMELINE over timeline use", "second", nil, "second", "env"),
			Entry("-t over CORONER_TIMELINE", "second", []string{"-t", "third"}, "third", "flag"),
		)

		It("exits 3 when CORONER_TIMELINE names a timeline that does not exist", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "first")).To(Equal(0))
			env["CORONER_TIMELINE"] = "missing"

			Expect(run(ctx, "status")).To(Equal(3))

			Expect(errOut.String()).To(ContainSubstring("CORONER_TIMELINE=missing"))
		})

		It("prints the database, schema and selected timeline as text", func(ctx SpecContext) {
			env["CORONER_TIMELINE"] = "second"

			Expect(run(ctx, "status")).To(Equal(0))

			Expect(out.String()).To(MatchRegexp(`(?m)^Database:\s+` + regexp.QuoteMeta(dbPath) + `$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Schema:\s+[1-9]\d*$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Timeline:\s+second \(selected by CORONER_TIMELINE\)$`))
		})

		It("exits 5 with kind db_too_new for a database a newer coroner migrated", func(ctx SpecContext) {
			db, err := sql.Open("sqlite", dbPath)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(db.Close)
			_, err = db.ExecContext(ctx,
				"INSERT INTO goose_db_version (version_id, is_applied) SELECT max(version_id) + 1, 1 FROM goose_db_version")
			Expect(err).NotTo(HaveOccurred())
			Expect(db.Close()).To(Succeed())

			Expect(run(ctx, "status", "--json")).To(Equal(5))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"db_too_new"`))
		})
	})
})
