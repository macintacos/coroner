package cli_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("timeline export", func() {
	var (
		out, errOut bytes.Buffer
		home        string
		ids         int
		now         time.Time
		tick        time.Duration
		outTTY      bool
		configFile  string
	)

	run := func(ctx SpecContext, args ...string) int {
		out.Reset()
		errOut.Reset()
		return cli.Run(ctx, cli.App{
			Args:      append([]string{"--db", filepath.Join(home, "coroner.db")}, args...),
			In:        strings.NewReader(""),
			Out:       &out,
			Err:       &errOut,
			OutIsTTY:  outTTY,
			LookupEnv: func(key string) (string, bool) { return map[string]string{"HOME": home}[key], key == "HOME" },
			ReadFile: func(string) ([]byte, error) {
				if configFile == "" {
					return nil, fs.ErrNotExist
				}
				return []byte(configFile), nil
			},
			Now: func() time.Time {
				now = now.Add(tick)
				return now
			},
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			},
			OpenStore: store.Open,
		})
	}
	exportAs := func(ctx SpecContext, format string) string {
		GinkgoHelper()
		Expect(run(ctx, "timeline", "export", "--format", format)).To(Equal(0), errOut.String())
		return out.String()
	}

	BeforeEach(func(ctx SpecContext) {
		home = GinkgoT().TempDir()
		ids = 0
		now = time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC)
		tick = 0
		outTTY = false
		configFile = ""
		for _, args := range [][]string{
			{"timeline", "create", "mongo-outage"},
			{"timeline", "use", "mongo-outage"},
			{"event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z", "--source", "mongod", "--author", "julian"},
			{"event", "add", "Pager fires for db-2", "--at", "2026-09-11T03:15:00Z", "--source", "pagerduty", "--author", "julian"},
			{"annotation", "add", "rs.status() showed no primary", "-e", "1", "--kind", "evidence", "--author", "julian"},
		} {
			Expect(run(ctx, args...)).To(Equal(0), errOut.String())
		}
	})

	DescribeTable("writes an unchanged timeline to the same bytes but for when it was exported",
		func(ctx SpecContext, format, exportedAtPattern string) {
			exportedAt := regexp.MustCompile(exportedAtPattern)
			first := exportAs(ctx, format)
			now = now.Add(time.Hour)

			second := exportAs(ctx, format)

			Expect(second).NotTo(Equal(first))
			Expect(exportedAt.ReplaceAllString(second, "")).To(Equal(exportedAt.ReplaceAllString(first, "")))
		},
		Entry("json", "json", `"exported_at":"[^"]*"`),
		Entry("jsonl", "jsonl", `"exported_at":"[^"]*"`),
		Entry("md", "md", `(?m)^- \*\*Exported:\*\*.*$`),
	)

	It("writes JSONL that decodes and re-encodes to the same bytes", func(ctx SpecContext) {
		jsonl := exportAs(ctx, "jsonl")

		records, err := export.Decode(strings.NewReader(jsonl))
		Expect(err).NotTo(HaveOccurred())
		var b bytes.Buffer
		Expect(export.WriteRecords(&b, records)).To(Succeed())

		Expect(b.String()).To(Equal(jsonl))
	})

	It("resolves a relative bound against the instant it records as exported at", func(ctx SpecContext) {
		instant := func(key string) time.Time {
			GinkgoHelper()
			match := regexp.MustCompile(`"` + key + `":"([^"]*)"`).FindStringSubmatch(out.String())
			Expect(match).To(HaveLen(2), out.String())
			t, err := time.Parse(time.RFC3339Nano, match[1])
			Expect(err).NotTo(HaveOccurred())
			return t
		}
		tick = time.Second

		Expect(run(ctx, "timeline", "export", "--json", "--since", "3 seconds ago")).To(Equal(0), errOut.String())

		Expect(instant("since")).To(Equal(instant("exported_at").Add(-3 * time.Second)))
	})

	It("indents JSON on a terminal", func(ctx SpecContext) {
		outTTY = true

		Expect(exportAs(ctx, "json")).To(HavePrefix("{\n  \"format\": \"coroner.export\",\n"))
	})

	It("writes JSON on one line when piped", func(ctx SpecContext) {
		Expect(strings.Count(exportAs(ctx, "json"), "\n")).To(Equal(1))
	})

	Describe("--format md", func() {
		BeforeEach(func() {
			configFile = `default_timezone = "America/New_York"`
		})

		It("shows times in UTC, whatever zone the config file sets", func(ctx SpecContext) {
			md := exportAs(ctx, "md")

			Expect(md).To(HavePrefix("# mongo-outage\n"))
			Expect(md).To(ContainSubstring("## 2026-09-11 (UTC)\n"))
			Expect(md).To(ContainSubstring("- **03:14:07** · `mongod` · Primary stepped down"))
		})

		It("shows times in the zone --tz names", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "md", "--tz", "America/New_York")).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring("## 2026-09-10 (EDT)\n"))
			Expect(out.String()).To(ContainSubstring("- **23:14:07** · `mongod` · Primary stepped down"))
		})

		It("reads --since in the config file's zone, as json and jsonl do", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "md", "--since", "2026-09-10 23:15")).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring("Pager fires for db-2"))
			Expect(out.String()).NotTo(ContainSubstring("Primary stepped down"))
		})

		It("sections events by source under --group-by source", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "md", "--group-by", "source")).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring("## `mongod`\n\n### 2026-09-11 (UTC)\n"))
		})

		It("adds each event's time as it was written under --include-raw", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "md", "--include-raw")).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring("Primary stepped down · as written: `2026-09-11T03:14:07Z`"))
		})
	})

	DescribeTable("exits 2, naming the flag, on a Markdown option it cannot apply",
		func(ctx SpecContext, flag string, args ...string) {
			Expect(run(ctx, append([]string{"timeline", "export"}, args...)...)).To(Equal(2))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(ContainSubstring(flag))
		},
		Entry("--group-by with json", "--group-by", "--format", "json", "--group-by", "source"),
		Entry("--include-raw with jsonl", "--include-raw", "--format", "jsonl", "--include-raw"),
		Entry("an unknown --group-by", "--group-by", "--format", "md", "--group-by", "kind"),
	)
})
