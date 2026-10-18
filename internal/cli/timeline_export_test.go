package cli_test

import (
	"bytes"
	"fmt"
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
			ReadFile:  readNoFile,
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
		func(ctx SpecContext, format string) {
			exportedAt := regexp.MustCompile(`"exported_at":"[^"]*"`)
			first := exportAs(ctx, format)
			now = now.Add(time.Hour)

			second := exportAs(ctx, format)

			Expect(second).NotTo(Equal(first))
			Expect(exportedAt.ReplaceAllString(second, "")).To(Equal(exportedAt.ReplaceAllString(first, "")))
		},
		Entry("json", "json"),
		Entry("jsonl", "jsonl"),
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
})
