package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
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
		return cli.Run(ctx, harness{
			home: env["HOME"], db: dbPath, env: env, out: &out, errOut: &errOut,
			now:   func() time.Time { return time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC) },
			newID: countedIDs(&ids),
		}.app(args...))
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

	It("exits 2 when -t is empty, before it looks for a database", func(ctx SpecContext) {
		Expect(run(ctx, "status", "-t", "")).To(Equal(2))
	})

	It("reports schema 0 and no timeline for a database behind the latest schema", func(ctx SpecContext) {
		Expect(os.WriteFile(dbPath, nil, 0o600)).To(Succeed())
		env["CORONER_TIMELINE"] = "first"

		Expect(run(ctx, "status", "--json")).To(Equal(0), errOut.String())

		Expect(out.String()).To(MatchJSON(fmt.Sprintf(
			`{"db_path": %q, "db_exists": true, "schema_version": 0, "timeline": null, "selected_by": null}`, dbPath)))
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

			Expect(errOut.String()).To(ContainSubstring(`CORONER_TIMELINE="missing"`))
		})

		It("exits 3 when -t names a timeline that does not exist", func(ctx SpecContext) {
			Expect(run(ctx, "status", "-t", "missing")).To(Equal(3))
		})

		It("prints the database, schema and selected timeline as text", func(ctx SpecContext) {
			env["CORONER_TIMELINE"] = "second"

			Expect(run(ctx, "status")).To(Equal(0))

			Expect(out.String()).To(MatchRegexp(`(?m)^Database:\s+` + regexp.QuoteMeta(dbPath) + `$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Schema:\s+[1-9]\d*$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Timeline:\s+second \(selected by CORONER_TIMELINE\)$`))
		})

		It("exits 5 with kind db_too_new for a database a newer coroner migrated", func(ctx SpecContext) {
			Expect(bumpSchemaVersion(ctx, dbPath)).To(Succeed())

			Expect(run(ctx, "status", "--json")).To(Equal(5))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"db_too_new"`))
		})
	})
})
