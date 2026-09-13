package store_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/tools/compatfixture/fixture"
)

var _ = Describe("Opening a database an earlier release wrote", func() {
	fixtures, _ := filepath.Glob("testdata/compat/*.db")

	for _, committed := range fixtures {
		It("migrates and reads "+filepath.Base(committed), func(ctx SpecContext) {
			content, err := os.ReadFile(filepath.Clean(committed))
			Expect(err).NotTo(HaveOccurred())
			path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
			Expect(os.WriteFile(path, content, 0o600)).To(Succeed())

			expectCompatFixture(ctx, path)
		})
	}

	// Until the first release commits its fixture, check the one this build writes.
	if len(fixtures) == 0 {
		It("reads a database compatfixture writes", func(ctx SpecContext) {
			path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
			Expect(fixture.Write(ctx, path)).To(Succeed())

			expectCompatFixture(ctx, path)
		})
	}
})

// expectCompatFixture opens the compatfixture database at path and reads back, through the
// store API, every kind of record it holds.
func expectCompatFixture(ctx context.Context, path string) {
	GinkgoHelper()
	s, err := store.Open(ctx, path)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(s.Close)

	timelines, err := s.Timelines(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(timelines).To(HaveExactElements(HaveField("Name", "inc-2041"), HaveField("Name", "inc-2042")))

	current, err := s.CurrentTimeline(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(current.Name).To(Equal("inc-2041"))
	Expect(current.Refs).To(Equal([]string{"INC-2041", "https://status.example.com/incidents/2041", "PM-77"}))

	events, err := s.Events(ctx, current.ID)
	Expect(err).NotTo(HaveOccurred())
	Expect(events).To(ConsistOf(
		HaveField("Precision", timeline.PrecisionDay),
		HaveField("Precision", timeline.PrecisionHour),
		HaveField("Precision", timeline.PrecisionMinute),
		HaveField("Precision", timeline.PrecisionSecond),
		HaveField("Precision", timeline.PrecisionMillisecond),
		HaveField("Precision", timeline.PrecisionMicrosecond),
		HaveField("Precision", timeline.PrecisionNanosecond),
	))
	Expect(events).To(SatisfyAll(
		ContainElement(HaveField("Approximate", true)),
		ContainElement(HaveField("EndedAt", HaveValue(BeTemporally("==", time.Date(2026, 9, 12, 16, 45, 0, 0, time.UTC))))),
		ContainElement(HaveField("OccurredAt", BeTemporally("==", time.Date(2026, 9, 12, 16, 24, 59, 123456789, time.UTC)))),
		ContainElement(HaveField("OccurredOffset", BeNil())),
		ContainElement(HaveField("OccurredOffset", HaveValue(Equal(-25200)))),
		ContainElement(HaveField("Source", BeNil())),
		ContainElement(HaveField("Source", HaveValue(Equal("pagerduty")))),
		ContainElement(HaveField("Tags", Equal([]string{"deploy", "rollback"}))),
	))

	annotations, err := s.Annotations(ctx, current.ID)
	Expect(err).NotTo(HaveOccurred())
	Expect(annotations).To(ConsistOf(
		SatisfyAll(HaveField("Kind", timeline.AnnotationEvidence), HaveField("EventID", Not(BeNil()))),
		SatisfyAll(HaveField("Kind", timeline.AnnotationComment), HaveField("EventID", Not(BeNil()))),
		HaveField("EventID", BeNil()),
	))

	other := timelines[1]
	Expect(s.Events(ctx, other.ID)).To(HaveLen(1))
	Expect(s.Annotations(ctx, other.ID)).To(BeEmpty())
}
