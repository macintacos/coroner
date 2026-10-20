package cli_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
)

var _ = Describe("event list", func() {
	var (
		out, errOut bytes.Buffer
		home        string
		ids         int
	)

	run := func(ctx SpecContext, args ...string) int {
		out.Reset()
		errOut.Reset()
		newYork, err := time.LoadLocation("America/New_York")
		Expect(err).NotTo(HaveOccurred())
		return cli.Run(ctx, harness{
			home: home, db: filepath.Join(home, "coroner.db"), out: &out, errOut: &errOut,
			now:   func() time.Time { return time.Date(2026, time.September, 12, 21, 30, 0, 0, newYork) },
			newID: countedIDs(&ids),
		}.app(args...))
	}

	var (
		seeded    cli.SeededHome
		seededIDs int
	)

	type page struct {
		Events []struct {
			Seq  int64  `json:"seq"`
			Type string `json:"type"`
		} `json:"events"`
		Truncated bool `json:"truncated"`
		Total     int  `json:"total"`
	}
	list := func(ctx SpecContext, args ...string) page {
		GinkgoHelper()
		Expect(run(ctx, append([]string{"event", "list", "--json"}, args...)...)).To(Equal(0), errOut.String())
		var p page
		Expect(json.Unmarshal(out.Bytes(), &p)).To(Succeed())
		return p
	}
	seqs := func(p page) []int64 {
		got := []int64{}
		for _, e := range p.Events {
			got = append(got, e.Seq)
		}
		return got
	}

	BeforeEach(func(ctx SpecContext) {
		home = GinkgoT().TempDir()
		ids = 0
		seeded.Into(home, func() {
			cli.WriteMigratedDatabase(filepath.Join(home, "coroner.db"))
			for _, args := range [][]string{
				{"timeline", "create", "mongo-outage"},
				{"timeline", "use", "mongo-outage"},
				{"event", "add", "Pager fires for db-2", "--at", "2026-09-11T03:00:00Z", "--source", "pagerduty", "--tag", "phase:detect"},
				{
					"event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07Z", "--source", "mongod",
					"--tag", "election", "--tag", "phase:detect",
				},
				{"event", "add", "New primary elected", "--at", "2026-09-11T03:14:07Z", "--source", "mongod", "--tag", "election"},
				{"event", "add", "Deploy rolled back", "--at", "2026-09-11T04:00:00Z", "--source", "slack"},
			} {
				Expect(run(ctx, args...)).To(Equal(0), errOut.String())
			}
			seededIDs = ids
		})
		ids = seededIDs
	})

	It("lists every event by occurred at, then seq, with no limit and nothing truncated", func(ctx SpecContext) {
		Expect(list(ctx)).To(SatisfyAll(
			WithTransform(seqs, Equal([]int64{1, 2, 3, 4})),
			HaveField("Truncated", BeFalse()),
			HaveField("Total", 4)))
	})

	It("reverses occurred at and seq together under --order desc", func(ctx SpecContext) {
		Expect(seqs(list(ctx, "--order", "desc"))).To(Equal([]int64{4, 3, 2, 1}))
	})

	It("keeps events at or after --since and before --until", func(ctx SpecContext) {
		Expect(seqs(list(ctx, "--since", "2026-09-11T03:14:07Z", "--until", "2026-09-11T04:00:00Z"))).
			To(Equal([]int64{2, 3}))
	})

	It("keeps nothing when --since and --until are the same time", func(ctx SpecContext) {
		Expect(seqs(list(ctx, "--since", "2026-09-11T03:14:07Z", "--until", "2026-09-11T03:14:07Z"))).To(BeEmpty())
	})

	It("keeps events from any --source given", func(ctx SpecContext) {
		Expect(seqs(list(ctx, "--source", "pagerduty", "--source", "slack"))).To(Equal([]int64{1, 4}))
	})

	It("keeps events carrying every --tag given", func(ctx SpecContext) {
		Expect(seqs(list(ctx, "--tag", "election", "--tag", "phase:detect"))).To(Equal([]int64{2}))
	})

	It("keeps events whose text contains --contains, ignoring case", func(ctx SpecContext) {
		Expect(seqs(list(ctx, "--contains", "PRIMARY"))).To(Equal([]int64{2, 3}))
	})

	It("cuts the list at --limit, reporting the total and saying so on stderr", func(ctx SpecContext) {
		Expect(list(ctx, "--limit", "2")).To(SatisfyAll(
			WithTransform(seqs, Equal([]int64{1, 2})),
			HaveField("Truncated", BeTrue()),
			HaveField("Total", 4)))
		Expect(errOut.String()).To(ContainSubstring("2 of 4"))
	})

	It("prints the timeline, then one event object per line, each typed, under --format jsonl", func(ctx SpecContext) {
		Expect(run(ctx, "event", "list", "--format", "jsonl")).To(Equal(0))

		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		Expect(lines).To(HaveLen(5))
		Expect(lines[0]).To(HavePrefix(`{"type":"timeline","id":`))
		for _, line := range lines[1:] {
			Expect(line).To(HavePrefix(`{"type":"event","id":`))
		}
	})

	It("prints tab-separated rows of seq, display-zone time, source, tags and text when piped", func(ctx SpecContext) {
		Expect(run(ctx, "event", "list", "--until", "2026-09-11T03:14:07Z")).To(Equal(0))

		Expect(out.String()).To(Equal("#1\t2026-09-11T03:00:00Z\tpagerduty\tphase:detect\tPager fires for db-2\n"))
	})

	It("suggests event add on stderr when the timeline has no events and no filter was given, whatever the order or limit", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "create", "disk-full")).To(Equal(0), errOut.String())

		Expect(run(ctx, "event", "list", "-t", "disk-full", "--order", "desc", "--limit", "5")).To(Equal(0))

		Expect(out.String()).To(BeEmpty())
		Expect(errOut.String()).To(Equal("No events yet. Add one with: coroner event add <text>\n"))
	})

	It("says on stderr that no events match when a filter leaves none", func(ctx SpecContext) {
		Expect(run(ctx, "event", "list", "--tag", "no-such-tag")).To(Equal(0))

		Expect(out.String()).To(BeEmpty())
		Expect(errOut.String()).To(Equal("No events match.\n"))
	})

	It("says on stderr which times --since and --until resolved to, unless --quiet", func(ctx SpecContext) {
		Expect(run(ctx, "event", "list", "--since", "2026-09-11 03:00", "--until", "2026-09-11T08:00:00Z")).To(Equal(0))
		Expect(errOut.String()).To(ContainSubstring("2026-09-11T03:00:00Z (+00:00 UTC: 03:00:00)"))
		Expect(errOut.String()).To(ContainSubstring("2026-09-11T08:00:00Z (+00:00 UTC: 08:00:00)"))

		Expect(run(ctx, "event", "list", "--since", "2026-09-11 03:00", "--quiet")).To(Equal(0))
		Expect(errOut.String()).To(BeEmpty())
	})

	DescribeTable("exits 2 on a filter no event could match, before reading the database",
		func(ctx SpecContext, kind string, args ...string) {
			Expect(run(ctx, append([]string{"event", "list", "--json"}, args...)...)).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"` + kind + `"`))
		},
		Entry("an --order other than asc or desc", "usage", "--order", "newest"),
		Entry("a --limit below 1", "usage", "--limit", "0"),
		Entry("a blank --contains", "usage", "--contains", " "),
		Entry("an invalid --source", "invalid_name", "--source", "DB 2"),
		Entry("an invalid --tag", "invalid_name", "--tag", "Election"),
		Entry("a --since it cannot read", "invalid_time", "--since", "the day before"),
	)
})
