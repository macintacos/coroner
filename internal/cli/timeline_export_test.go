package cli_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
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
			WriteFile: os.WriteFile,
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

		It("ends with the timeline's Mermaid diagram under --diagram", func(ctx SpecContext) {
			diagram := exportAs(ctx, "mermaid")

			Expect(run(ctx, "timeline", "export", "--format", "md", "--diagram")).To(Equal(0), errOut.String())

			Expect(out.String()).To(HaveSuffix("\n```mermaid\n" + diagram + "```\n"))
		})
	})

	Describe("--format mermaid", func() {
		BeforeEach(func() {
			configFile = `default_timezone = "America/New_York"`
		})

		It("draws a gantt in UTC, whatever zone the config file sets", func(ctx SpecContext) {
			diagram := exportAs(ctx, "mermaid")

			Expect(diagram).To(HavePrefix("gantt\n  title mongo-outage (UTC)\n"))
			Expect(diagram).To(ContainSubstring("  section mongod\n    Primary stepped down :milestone, 2026-09-11T03:14:07, 0s\n"))
			Expect(errOut.String()).To(BeEmpty())
		})

		It("draws the times on the wall clock of the zone --tz names", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "mermaid", "--tz", "America/New_York")).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring("  title mongo-outage (EDT)\n"))
			Expect(out.String()).To(ContainSubstring(":milestone, 2026-09-10T23:14:07, 0s\n"))
		})

		It("writes an unchanged timeline to the same bytes whenever it is exported", func(ctx SpecContext) {
			first := exportAs(ctx, "mermaid")
			now = now.Add(time.Hour)

			Expect(exportAs(ctx, "mermaid")).To(Equal(first))
		})
	})

	DescribeTable("warns on stderr, still writing the whole diagram, when it is too long for Mermaid to draw",
		func(ctx SpecContext, args ...string) {
			text := strings.Repeat("x", export.MermaidMaxTextSize)
			Expect(run(ctx, "event", "add", text, "--at", "2026-09-11T03:16:00Z")).To(Equal(0), errOut.String())

			Expect(run(ctx, append([]string{"timeline", "export"}, args...)...)).To(Equal(0), errOut.String())

			Expect(out.String()).To(ContainSubstring(text + " :milestone, 2026-09-11T03:16:00, 0s\n"))
			Expect(errOut.String()).To(ContainSubstring("--since, --until or --source"))
		},
		Entry("--format mermaid", "--format", "mermaid"),
		Entry("--format md --diagram", "--format", "md", "--diagram"),
	)

	DescribeTable("exits 2, naming the flag, on a Markdown option it cannot apply",
		func(ctx SpecContext, flagName string, args ...string) {
			Expect(run(ctx, append([]string{"timeline", "export"}, args...)...)).To(Equal(2))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(ContainSubstring(flagName))
		},
		Entry("--group-by with json", "--group-by", "--format", "json", "--group-by", "source"),
		Entry("--include-raw with jsonl", "--include-raw", "--format", "jsonl", "--include-raw"),
		Entry("an unknown --group-by", "--group-by", "--format", "md", "--group-by", "kind"),
		Entry("an empty --group-by", "--group-by", "--format", "md", "--group-by", ""),
		Entry("--diagram with json", "--diagram", "--format", "json", "--diagram"),
		Entry("--diagram with mermaid", "--diagram", "--format", "mermaid", "--diagram"),
		Entry("--group-by with html", "--group-by", "--format", "html", "-o", "-", "--group-by", "source"),
	)

	Describe("-o", func() {
		It("writes the export to the file it names, as piped output, and says so on stderr", func(ctx SpecContext) {
			piped := exportAs(ctx, "json")
			path := filepath.Join(home, "rca.json")
			outTTY = true

			Expect(run(ctx, "timeline", "export", "--format", "json", "-o", path)).To(Equal(0), errOut.String())

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(ContainSubstring(path))
			Expect(readFile(path)).To(Equal(piped))
		})

		It("says nothing on stderr under --quiet", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "jsonl", "-o", filepath.Join(home, "rca.jsonl"), "--quiet")).To(Equal(0), errOut.String())

			Expect(errOut.String()).To(BeEmpty())
		})

		It("writes to stdout when it is -", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "md", "-o", "-")).To(Equal(0), errOut.String())

			Expect(out.String()).To(HavePrefix("# mongo-outage\n"))
		})

		It("exits 2 when it is empty", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "json", "-o", "")).To(Equal(2))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(ContainSubstring("-o"))
		})

		It("exits 2, naming the file, when its directory does not exist", func(ctx SpecContext) {
			path := filepath.Join(home, "missing", "rca.json")

			Expect(run(ctx, "timeline", "export", "--format", "json", "-o", path)).To(Equal(2))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(ContainSubstring(path))
		})
	})

	Describe("--format html", func() {
		It("writes one page embedding the export to the file -o names", func(ctx SpecContext) {
			path := filepath.Join(home, "inc-4821.html")

			Expect(run(ctx, "timeline", "export", "--format", "html", "-o", path)).To(Equal(0), errOut.String())

			page := readFile(path)
			Expect(page).To(HavePrefix("<!doctype html>"))
			Expect(page).To(ContainSubstring(`"text":"Primary stepped down"`))
		})

		It("exits 2, naming -o, without it", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "export", "--format", "html")).To(Equal(2))

			Expect(out.String()).To(BeEmpty())
			Expect(errOut.String()).To(ContainSubstring("-o"))
		})

		It("shows times in UTC unless --tz names a zone", func(ctx SpecContext) {
			configFile = `default_timezone = "America/New_York"`

			Expect(run(ctx, "timeline", "export", "--format", "html", "-o", "-")).To(Equal(0), errOut.String())
			Expect(out.String()).To(ContainSubstring(`id="coroner-zone">{"name":"UTC",`))

			Expect(run(ctx, "timeline", "export", "--format", "html", "-o", "-", "--tz", "Asia/Tokyo")).To(Equal(0), errOut.String())
			Expect(out.String()).To(ContainSubstring(`id="coroner-zone">{"name":"Asia/Tokyo",`))
		})
	})
})

func readFile(path string) string {
	GinkgoHelper()
	b, err := os.ReadFile(filepath.Clean(path))
	Expect(err).NotTo(HaveOccurred())

	return string(b)
}
