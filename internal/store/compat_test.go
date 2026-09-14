package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
	"github.com/macintacos/coroner/internal/tools/compatfixture/fixture"
)

var _ = Describe("Opening a database an earlier release wrote", func() {
	fixtures, _ := filepath.Glob("testdata/compat/v*.db")

	for _, committed := range fixtures {
		It("migrates and reads "+filepath.Base(committed), func(ctx SpecContext) {
			content, err := os.ReadFile(filepath.Clean(committed))
			Expect(err).NotTo(HaveOccurred())
			path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
			Expect(os.WriteFile(path, content, 0o600)).To(Succeed())

			expectCompatFixture(ctx, path)
		})
	}

	// The next release's fixture is this build's output, so it must pass the same checks.
	It("reads a database compatfixture writes", func(ctx SpecContext) {
		path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
		Expect(fixture.Write(ctx, path)).To(Succeed())

		expectCompatFixture(ctx, path)
	})

	migrations, _ := filepath.Glob("migrations/*.sql")
	for schema := int64(1); schema < int64(len(migrations)); schema++ {
		It(fmt.Sprintf("upgrades a schema %d database compatfixture filled, backing it up first", schema), func(ctx SpecContext) {
			path := filepath.Join(GinkgoT().TempDir(), "coroner.db")
			s, err := store.OpenWithMigrations(ctx, path, schemaUpTo(schema))
			Expect(err).NotTo(HaveOccurred())
			Expect(fixture.Fill(ctx, s)).To(Succeed())
			Expect(s.Close()).To(Succeed())

			expectCompatFixture(ctx, path)

			backups, err := filepath.Glob(path + ".pre-v*.bak")
			Expect(err).NotTo(HaveOccurred())
			Expect(backups).To(HaveLen(1))
			backup := backups[0]
			Expect(querySQLite(ctx, backup, "SELECT max(version_id) FROM goose_db_version")).To(Equal(schema))
			Expect(querySQLite(ctx, backup, "SELECT count(*) FROM event")).To(Equal(int64(len(fixture.Events()))))
		})
	}
})

// expectCompatFixture opens the compatfixture database at path, reads back every record
// fixture writes, adds an event to check that the timeline's seq counter survived, and saves
// an overlay and a link across the fixture's timelines.
func expectCompatFixture(ctx context.Context, path string) {
	GinkgoHelper()
	s, err := store.Open(ctx, path)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(s.Close)

	timelines := fixture.Timelines()
	Expect(s.Timelines(ctx)).To(Equal(timelines))
	Expect(s.Sources(ctx)).To(Equal(fixture.Sources()))
	Expect(s.CurrentTimeline(ctx)).To(Equal(timelines[0]))
	for _, t := range timelines {
		Expect(s.Events(ctx, t.ID)).To(Equal(slices.DeleteFunc(fixture.Events(),
			func(e timeline.Event) bool { return e.TimelineID != t.ID })))
		Expect(s.Annotations(ctx, t.ID)).To(Equal(slices.DeleteFunc(fixture.Annotations(),
			func(a timeline.Annotation) bool { return a.TimelineID != t.ID })))
	}

	at := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	added, err := s.AddEvent(ctx, timeline.Event{
		ID: uuid(100), TimelineID: timelines[0].ID, OccurredAt: at, Precision: timeline.PrecisionMinute,
		Tags: []string{}, Text: "Postmortem held", CreatedAt: at, UpdatedAt: at,
	}, "")
	Expect(err).NotTo(HaveOccurred())
	Expect(added.Seq).To(Equal(int64(8)))

	both := timeline.Overlay{ID: uuid(101), Name: "inc-2041-2042", TimelineIDs: []string{timelines[1].ID, timelines[0].ID}, CreatedAt: at}
	Expect(s.CreateOverlay(ctx, both)).To(Succeed())
	Expect(s.OverlayByName(ctx, both.Name)).To(Equal(both))
	events := fixture.Events()
	link := timeline.Link{ID: uuid(102), FromEventID: events[len(events)-1].ID, ToEventID: added.ID, Kind: timeline.LinkLedTo, CreatedAt: at}
	Expect(s.AddLink(ctx, link)).To(Succeed())
	Expect(s.EventLinks(ctx, added.ID)).To(Equal([]timeline.Link{link}))
}
