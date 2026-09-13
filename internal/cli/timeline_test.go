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
	"github.com/macintacos/coroner/internal/timeline"
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

	Describe("view", func() {
		const timelineID = "0199a1b2-0000-7000-8000-000000000001"

		addEvent := func(s *store.Store, ctx SpecContext, n int, at time.Time, source, text string, tags ...string) {
			GinkgoHelper()
			_, err := s.AddEvent(ctx, timeline.Event{
				ID:         fmt.Sprintf("0199a1b2-0000-7000-8000-1000000000%02d", n),
				TimelineID: timelineID,
				OccurredAt: at,
				Precision:  timeline.PrecisionSecond,
				Source:     &source,
				Tags:       append([]string{}, tags...),
				Text:       text,
				CreatedAt:  now,
				UpdatedAt:  now,
			}, fmt.Sprintf("0199a1b2-0000-7000-8000-2000000000%02d", n))
			Expect(err).NotTo(HaveOccurred())
		}

		BeforeEach(func(ctx SpecContext) {
			run(ctx, "timeline", "create", "mongo-outage", "--title", "Primary election storm", "--ref", "INC-4821")

			s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(s.Close)
			// Added out of time order, so seq order and time order differ.
			addEvent(s, ctx, 1, time.Date(2026, 9, 11, 3, 15, 0, 0, time.UTC), "slack", "Pager fires for db-2")
			addEvent(s, ctx, 2, time.Date(2026, 9, 11, 3, 14, 0, 0, time.UTC), "mongod", "Primary stepped down", "election")
			addEvent(s, ctx, 3, time.Date(2026, 9, 11, 4, 20, 30, 0, time.UTC), "mongod", "New primary elected", "election")
			eventID := "0199a1b2-0000-7000-8000-100000000002"
			Expect(s.AddAnnotation(ctx, timeline.Annotation{
				ID: "0199a1b2-0000-7000-8000-300000000001", TimelineID: timelineID, EventID: &eventID,
				Kind: timeline.AnnotationEvidence, Body: "replSetStepDown", CreatedAt: now, UpdatedAt: now,
			}, "0199a1b2-0000-7000-8000-400000000001")).To(Succeed())
		})

		It("lists the events in time order, each with the gap since the one before", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "view", "mongo-outage")).To(Equal(0))

			Expect(out.String()).To(MatchRegexp(`(?s)` +
				`\n#2 +2026-09-11T03:14:00\.000000000Z +mongod +Primary stepped down\n` +
				`#1 +2026-09-11T03:15:00\.000000000Z +\+1m0s +slack +Pager fires for db-2\n` +
				`#3 +2026-09-11T04:20:30\.000000000Z +\+1h5m30s +mongod +New primary elected\n$`))
		})

		It("summarizes the timeline before its events", func(ctx SpecContext) {
			run(ctx, "timeline", "view", "mongo-outage")

			Expect(out.String()).To(HavePrefix("mongo-outage\n"))
			Expect(out.String()).To(MatchRegexp(`(?m)^Title: +Primary election storm$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Refs: +INC-4821$`))
			Expect(out.String()).To(MatchRegexp(
				`(?m)^Events: +3, from 2026-09-11T03:14:00\.000000000Z to 2026-09-11T04:20:30\.000000000Z$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Sources: +mongod 2, slack 1$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Tags: +election 2$`))
			Expect(out.String()).To(MatchRegexp(`(?m)^Annotations: +1$`))
		})

		It("prints the bare timeline object under --json", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "view", "mongo-outage", "--json")).To(Equal(0))

			Expect(out.String()).To(MatchJSON(`{
				"id": "0199a1b2-0000-7000-8000-000000000001",
				"name": "mongo-outage",
				"title": "Primary election storm",
				"refs": ["INC-4821"],
				"created_at": "2026-09-11T03:00:00.000000000Z"
			}`))
		})

		It("views the current timeline when none is named", func(ctx SpecContext) {
			run(ctx, "timeline", "use", "mongo-outage")

			Expect(run(ctx, "timeline", "view")).To(Equal(0))
			Expect(out.String()).To(HavePrefix("mongo-outage\n"))
		})

		It("exits 2 when no timeline is named and none is current", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "view", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"timeline_required"`))
		})

		It("exits 3 for a timeline that does not exist", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "view", "mongo")).To(Equal(3))
		})
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
