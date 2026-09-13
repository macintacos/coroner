package cli_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
)

var _ = Describe("timeline", func() {
	var (
		out, errOut bytes.Buffer
		home        string
		now         time.Time
		ids         int
	)

	run := func(ctx SpecContext, args ...string) int {
		out.Reset()
		errOut.Reset()
		return cli.Run(ctx, cli.App{
			Args: append([]string{"--db", filepath.Join(home, "coroner.db")}, args...),
			Out:  &out,
			Err:  &errOut,
			LookupEnv: func(key string) (string, bool) {
				if key == "HOME" {
					return home, true
				}
				return "", false
			},
			ReadFile: readNoFile,
			Now:      func() time.Time { return now },
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("0199a1b2-0000-7000-8000-%012d", ids))
			},
			OpenStore: store.Open,
		})
	}

	BeforeEach(func() {
		home = GinkgoT().TempDir()
		now = time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)
		ids = 0
	})

	It("prints its help when run without a verb", func(ctx SpecContext) {
		Expect(run(ctx, "timeline")).To(Equal(0))

		Expect(out.String()).To(ContainSubstring("coroner timeline <command>"))
	})

	It("rejects a verb it does not have with exit 2 and a suggestion", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "craete")).To(Equal(2))

		Expect(errOut.String()).To(ContainSubstring(`unknown command "craete" for "coroner timeline"`))
		Expect(errOut.String()).To(MatchRegexp(`Did you mean this\?\n\s+create`))
	})

	Describe("create", func() {
		It("prints the created timeline as JSON and says what changed on stderr", func(ctx SpecContext) {
			code := run(ctx, "timeline", "create", "mongo-outage", "--title", "Primary election storm",
				"--ref", "INC-4821", "--ref", "https://example.com/INC-4821", "--json")

			Expect(code).To(Equal(0))
			Expect(out.String()).To(MatchJSON(`{
				"id": "0199a1b2-0000-7000-8000-000000000001",
				"name": "mongo-outage",
				"title": "Primary election storm",
				"refs": ["INC-4821", "https://example.com/INC-4821"],
				"created_at": "2026-09-11T03:00:00.000000000Z"
			}`))
			Expect(errOut.String()).To(ContainSubstring("mongo-outage"))
			Expect(errOut.String()).To(ContainSubstring("coroner timeline use mongo-outage"))
		})

		It("prints nothing on stderr under --quiet", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "mongo-outage", "-q")).To(Equal(0))

			Expect(errOut.String()).To(BeEmpty())
		})

		It("keeps a repeated ref once, in the order first given", func(ctx SpecContext) {
			run(ctx, "timeline", "create", "mongo-outage", "--ref", "INC-2", "--ref", "INC-1", "--ref", "INC-2", "--json")

			Expect(out.String()).To(ContainSubstring(`"refs":["INC-2","INC-1"]`))
		})

		It("rejects an invalid name with exit 2, a suggestion, and no database written", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "create", "Mongo Outage")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring("mongo-outage"))
			Expect(filepath.Join(home, "coroner.db")).NotTo(BeAnExistingFile())
		})

		It("refuses a name that is taken with exit 4", func(ctx SpecContext) {
			run(ctx, "timeline", "create", "mongo-outage")

			Expect(run(ctx, "timeline", "create", "mongo-outage", "--title", "again")).To(Equal(4))
		})

		It("prints the existing timeline under --if-not-exists", func(ctx SpecContext) {
			run(ctx, "timeline", "create", "mongo-outage", "--json")
			created := out.String()
			now = now.Add(time.Hour)

			Expect(run(ctx, "timeline", "create", "mongo-outage", "--if-not-exists", "--json")).To(Equal(0))
			Expect(out.String()).To(Equal(created))
		})

		It("leaves the current timeline alone", func(ctx SpecContext) {
			run(ctx, "timeline", "create", "first")
			run(ctx, "timeline", "use", "first")
			run(ctx, "timeline", "create", "second")

			run(ctx, "timeline", "list")

			Expect(out.String()).To(MatchRegexp(`(?m)^\* first\t`))
		})
	})

	Describe("list", func() {
		BeforeEach(func(ctx SpecContext) {
			run(ctx, "timeline", "create", "zeta", "--title", "oldest")
			now = now.Add(time.Minute)
			run(ctx, "timeline", "create", "alpha")
			run(ctx, "timeline", "create", "beta", "--title", "same instant, higher id")
		})

		It("prints one row per timeline in creation order, with no marker while none is in use", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "list")).To(Equal(0))

			Expect(out.String()).To(Equal("  zeta\toldest\n  alpha\t\n  beta\tsame instant, higher id\n"))
		})

		It("marks the timeline in use", func(ctx SpecContext) {
			run(ctx, "timeline", "use", "alpha")

			run(ctx, "timeline", "list")

			Expect(out.String()).To(Equal("  zeta\toldest\n* alpha\t\n  beta\tsame instant, higher id\n"))
		})

		It("flags the timeline in use in JSON", func(ctx SpecContext) {
			run(ctx, "timeline", "use", "beta")

			Expect(run(ctx, "timeline", "list", "--json")).To(Equal(0))

			Expect(out.String()).To(MatchRegexp(
				`^\{"timelines":\[\{"id":"[^"]+","name":"zeta",[^}]*"current":false\},` +
					`\{[^}]*"name":"alpha",[^}]*"current":false\},\{[^}]*"name":"beta",[^}]*"current":true\}\]\}\n$`))
		})
	})

	It("lists nothing and hints at create when there are no timelines", func(ctx SpecContext) {
		Expect(run(ctx, "timeline", "list", "--json")).To(Equal(0))
		Expect(out.String()).To(Equal(`{"timelines":[]}` + "\n"))

		Expect(run(ctx, "timeline", "list")).To(Equal(0))
		Expect(out.String()).To(BeEmpty())
		Expect(errOut.String()).To(ContainSubstring("coroner timeline create"))
	})

	Describe("use", func() {
		BeforeEach(func(ctx SpecContext) {
			run(ctx, "timeline", "create", "mongo-outage")
		})

		It("selects a timeline by id as well as by name", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "0199a1b2-0000-7000-8000-000000000001")).To(Equal(0))

			run(ctx, "timeline", "list")
			Expect(out.String()).To(HavePrefix("* mongo-outage"))
		})

		It("clears the selection with --clear", func(ctx SpecContext) {
			run(ctx, "timeline", "use", "mongo-outage")

			Expect(run(ctx, "timeline", "use", "--clear")).To(Equal(0))

			run(ctx, "timeline", "list")
			Expect(out.String()).To(HavePrefix("  mongo-outage"))
		})

		DescribeTable("exits 2 unless given exactly one of a timeline or --clear",
			func(ctx SpecContext, args ...string) {
				Expect(run(ctx, append([]string{"timeline", "use"}, args...)...)).To(Equal(2))
			},
			Entry("neither"),
			Entry("both", "mongo-outage", "--clear"),
		)

		It("exits 3 for a timeline that does not exist", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "mongo")).To(Equal(3))
		})
	})
})

func readNoFile(string) ([]byte, error) {
	return nil, fs.ErrNotExist
}
