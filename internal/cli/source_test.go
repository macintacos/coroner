package cli_test

import (
	"bytes"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/cli"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("source list", func() {
	var (
		out, errOut bytes.Buffer
		home        string
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
			ReadFile:  readNoFile,
			Now:       func() time.Time { return time.Date(2026, time.September, 13, 1, 30, 0, 0, time.UTC) },
			OpenStore: store.Open,
		})
	}

	BeforeEach(func(ctx SpecContext) {
		home = GinkgoT().TempDir()
		created := time.Date(2026, time.September, 11, 3, 0, 0, 0, time.UTC)
		s, err := store.Open(ctx, filepath.Join(home, "coroner.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)

		incident := timeline.Timeline{ID: "0199a1b2-0000-7000-8000-000000000001", Name: "mongo-outage", CreatedAt: created, UpdatedAt: created}
		Expect(s.CreateTimeline(ctx, incident)).To(Succeed())
		annotation := timeline.Annotation{
			ID: "0199a1b2-0000-7000-8000-000000000002", TimelineID: incident.ID, Kind: timeline.AnnotationEvidence,
			Body: "#inc-4821: primary is unreachable", Source: new("slack"), CreatedAt: created, UpdatedAt: created,
		}
		Expect(s.AddAnnotation(ctx, annotation, "0199a1b2-0000-7000-8000-000000000003")).To(Succeed())
	})

	Describe("a source no event names", func() {
		It("prints zero events and null occurred at as JSON", func(ctx SpecContext) {
			Expect(run(ctx, "source", "list", "--json")).To(Equal(0))

			Expect(out.String()).To(MatchJSON(`{"sources": [{
				"id": "0199a1b2-0000-7000-8000-000000000003",
				"name": "slack",
				"created_at": "2026-09-11T03:00:00.000000000Z",
				"event_count": 0,
				"annotation_count": 1,
				"first_occurred_at": null,
				"last_occurred_at": null
			}]}`))
		})

		It("prints a dash for each missing occurred at as text", func(ctx SpecContext) {
			Expect(run(ctx, "source", "list")).To(Equal(0))

			Expect(out.String()).To(Equal("slack\t0\t1\t-\t-\n"))
		})
	})
})
