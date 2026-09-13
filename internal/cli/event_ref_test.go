package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("event view, edit and delete", func() {
	// Ids differ from their eighth digit, so a prefix can name one event or several.
	const (
		steppedDown = "0199a1b4-0000-7000-8000-000000000000"
		pagerFires  = "0199a1b6-0000-7000-8000-000000000000"
		drill       = "0199a1b8-0000-7000-8000-000000000000"
	)

	var (
		out, errOut bytes.Buffer
		home        string
		ids         int
		in          io.Reader
		env         map[string]string
		now         time.Time
	)

	run := func(ctx SpecContext, args ...string) int {
		out.Reset()
		errOut.Reset()
		return cli.Run(ctx, cli.App{
			Args: append([]string{"--db", filepath.Join(home, "coroner.db")}, args...),
			In:   in,
			Out:  &out,
			Err:  &errOut,
			LookupEnv: func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			},
			ReadFile: readNoFile,
			Now:      func() time.Time { return now },
			NewID: func() (uuid.UUID, error) {
				ids++
				return uuid.Parse(fmt.Sprintf("%08x-0000-7000-8000-000000000000", 0x0199a1b2+ids))
			},
			OpenStore: store.Open,
		})
	}
	withStore := func(ctx SpecContext) *store.Store {
		GinkgoHelper()
		s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		return s
	}

	BeforeEach(func(ctx SpecContext) {
		home = GinkgoT().TempDir()
		ids = 0
		in = strings.NewReader("")
		env = map[string]string{"HOME": home}
		newYork, err := time.LoadLocation("America/New_York")
		Expect(err).NotTo(HaveOccurred())
		now = time.Date(2026, time.September, 12, 21, 30, 0, 0, newYork)
		for _, args := range [][]string{
			{"timeline", "create", "mongo-outage"},
			{"timeline", "use", "mongo-outage"},
			{"event", "add", "Primary stepped down", "--at", "2026-09-11T03:14:07.412Z", "--source", "mongod", "--tag", "election"},
			{"event", "add", "Pager fires for db-2", "--at", "2026-09-11T03:15:00Z"},
			{"timeline", "create", "mongo-failover"},
			{"event", "add", "Failover drill", "-t", "mongo-failover", "--at", "2026-09-11T05:00:00Z"},
		} {
			Expect(run(ctx, args...)).To(Equal(0), errOut.String())
		}
	})

	Describe("view", func() {
		DescribeTable("finds an event by seq, a timeline's seq, its id or a unique start of its id",
			func(ctx SpecContext, ref, id string) {
				Expect(run(ctx, "event", "view", ref, "--json")).To(Equal(0), errOut.String())

				Expect(out.String()).To(HavePrefix(`{"id":"` + id + `"`))
			},
			Entry("a seq in the current timeline", "2", pagerFires),
			Entry("a seq in a named timeline", "mongo-failover#1", drill),
			Entry("a full id, from any timeline", drill, drill),
			Entry("a unique start of an id, in any case", "0199A1B4", steppedDown),
		)

		It("finds a seq in the timeline -t names", func(ctx SpecContext) {
			Expect(run(ctx, "event", "view", "1", "-t", "mongo-failover", "--json")).To(Equal(0))

			Expect(out.String()).To(HavePrefix(`{"id":"` + drill + `"`))
		})

		It("needs no selected timeline for an id, only for a bare seq", func(ctx SpecContext) {
			Expect(run(ctx, "timeline", "use", "--clear")).To(Equal(0))

			Expect(run(ctx, "event", "view", steppedDown, "--json")).To(Equal(0))
			Expect(run(ctx, "event", "view", "1", "--json")).To(Equal(2))
			Expect(errOut.String()).To(ContainSubstring(`"kind":"timeline_required"`))
		})

		It("exits 2 with kind ambiguous_id, listing each match as timeline#seq, on a start several ids share",
			func(ctx SpecContext) {
				Expect(run(ctx, "event", "view", "0199a1b", "--json")).To(Equal(2))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"ambiguous_id"`))
				Expect(errOut.String()).To(ContainSubstring("mongo-outage#1  " + steppedDown))
				Expect(errOut.String()).To(ContainSubstring("mongo-failover#1  " + drill))
			})

		DescribeTable("exits 3 with kind event_not_found on an event that does not exist",
			func(ctx SpecContext, ref string) {
				Expect(run(ctx, "event", "view", ref, "--json")).To(Equal(3))

				Expect(errOut.String()).To(ContainSubstring(`"kind":"event_not_found"`))
			},
			Entry("a seq past the last", "9"),
			Entry("an id no event has", "0199a1c"),
		)

		It("exits 2 with kind usage on something that is not an event reference", func(ctx SpecContext) {
			Expect(run(ctx, "event", "view", "#2", "--json")).To(Equal(2))

			Expect(errOut.String()).To(ContainSubstring(`"kind":"usage"`))
		})

		It("describes the event and lists its annotations in text, with times in the display zone",
			func(ctx SpecContext) {
				eventID := steppedDown
				Expect(withStore(ctx).AddAnnotation(ctx, timeline.Annotation{
					ID: "0199a1c0-0000-7000-8000-000000000000", TimelineID: "0199a1b3-0000-7000-8000-000000000000",
					EventID: &eventID, Kind: timeline.AnnotationEvidence, Body: "replSetStepDown", CreatedAt: now, UpdatedAt: now,
				}, "0199a1c1-0000-7000-8000-000000000000")).To(Succeed())

				Expect(run(ctx, "event", "view", "1")).To(Equal(0))

				Expect(out.String()).To(HavePrefix("Event #1 in timeline `mongo-outage`\n"))
				Expect(out.String()).To(MatchRegexp(`(?m)^Occurred: +2026-09-10T23:14:07\.412-04:00 \(millisecond\)$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Source: +mongod$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Text: +Primary stepped down$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^Annotations: +1$`))
				Expect(out.String()).To(MatchRegexp(`(?m)^evidence\t2026-09-12T21:30:00-04:00\t-\t-\treplSetStepDown$`))
			})
	})
})
