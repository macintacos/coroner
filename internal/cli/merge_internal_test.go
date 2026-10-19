package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/config"
	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("merge", func() {
	var (
		s      *store.Store
		dbPath string
		tl     export.Timeline
	)

	BeforeEach(func(ctx SpecContext) {
		dbPath = filepath.Join(GinkgoT().TempDir(), "coroner.db")
		var err error
		s, err = store.Open(ctx, dbPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
		tl = planTimeline(1, "inc-4821")
		Expect(s.CreateTimeline(ctx, tl.Domain())).To(Succeed())
	})

	// seed stores an event of tl with no source, and attachments with their content.
	seed := func(ctx context.Context, e export.Event, attachments []export.Attachment, blobs ...[]byte) {
		GinkgoHelper()
		e.Source = nil
		records := store.ImportRecords{Events: []timeline.Event{e.Domain()}}
		readers := make([]io.Reader, 0, len(blobs))
		for _, a := range attachments {
			records.Attachments = append(records.Attachments, a.Domain())
		}
		for _, b := range blobs {
			readers = append(readers, bytes.NewReader(b))
		}
		_, err := s.Import(ctx, records, readers...)
		Expect(err).NotTo(HaveOccurred())
	}

	inputOf := func(records ...export.Record) mergeInput {
		return mergeInput{
			importInput: planInput(records),
			timelines:   timelineInput{rule: timelineRule{guard: true}},
			lookupEnv:   func(string) (string, bool) { return "", false },
		}
	}

	It("returns the inserted events with their seqs", func(ctx SpecContext) {
		stored := planEvent(2, tl.ID, "Primary steps down")
		stored.Seq = new(int64(1))
		seed(ctx, stored, nil)
		reached := planEvent(8, tl.ID, "Secondary takes over")
		reached.Seq = new(int64(1))
		unnumbered := planEvent(9, tl.ID, "Clients reconnect")
		unnumbered.Seq = nil
		unnumbered.Tags = []string{"net", "client"}

		out, err := merge(ctx, s, inputOf(planEvent(7, tl.ID, "Pager fires"), reached, unnumbered), mergeWrite)

		Expect(err).NotTo(HaveOccurred())
		Expect(out.inserted).To(HaveLen(3))
		for _, e := range out.inserted {
			read, err := s.Event(ctx, e.ID)
			Expect(err).NotTo(HaveOccurred())
			want, err := json.Marshal(export.NewEvent(read))
			Expect(err).NotTo(HaveOccurred())
			Expect(json.Marshal(export.NewEvent(e))).To(MatchJSON(want))
		}
	})

	It("writes nothing and reports blocked as data when a record conflicts under fail", func(ctx SpecContext) {
		e := planEvent(2, tl.ID, "Primary steps down")
		seed(ctx, e, nil)
		before, err := s.Event(ctx, e.ID)
		Expect(err).NotTo(HaveOccurred())
		e.Source = nil
		e.Text = "Primary steps down hard"

		out, err := merge(ctx, s, inputOf(e), mergeWrite)

		Expect(err).NotTo(HaveOccurred())
		Expect(out.plan.blocked).To(Equal(blockConflicts))
		Expect(s.Event(ctx, e.ID)).To(Equal(before))
	})

	It("plans only, in a read transaction, under mergePlanOnly: it writes nothing and waits on no writer", func(ctx SpecContext) {
		other, err := store.Open(ctx, dbPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(other.Close)
		inside, release, changed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		closeRelease := sync.OnceFunc(func() { close(release) })
		DeferCleanup(closeRelease)
		go func() {
			_, err := other.Change(ctx, store.ChangeOptions{Mode: store.Commit}, func(tx *store.Tx) error {
				err := tx.CreateTimeline(ctx, planTimeline(3, "inc-4822").Domain())
				close(inside)
				<-release
				return err
			})
			changed <- err
		}()
		Eventually(ctx, inside).Should(BeClosed())

		merged := make(chan mergeOutcome, 1)
		first, second := planEvent(7, tl.ID, "Pager fires"), planEvent(8, tl.ID, "Secondary takes over")
		go func() {
			defer GinkgoRecover()
			out, err := merge(ctx, s, inputOf(first, second), mergePlanOnly)
			Expect(err).NotTo(HaveOccurred())
			merged <- out
		}()
		var out mergeOutcome
		Eventually(merged).WithTimeout(time.Second).Should(Receive(&out))
		closeRelease()
		Eventually(ctx, changed).Should(Receive(BeNil()))

		Expect(out.plan.result.Events.Inserted).To(Equal(2))
		Expect(out.inserted).To(HaveEach(HaveField("Seq", BeZero())))
		for _, id := range []string{first.ID, second.ID} {
			_, err := s.Event(ctx, id)
			Expect(err).To(MatchError(store.ErrNotFound))
		}
	}, SpecTimeout(time.Minute))

	It("names only the sidecar blobs the plan writes, and leaves no temp file", func(ctx SpecContext) {
		e := planEvent(2, tl.ID, "Primary steps down")
		kept, added := []byte("rs.status() before"), []byte("rs.status() after")
		stored, incoming := planAttachment(3, kept), planAttachment(4, added)
		stored.EventID, incoming.EventID = &e.ID, &e.ID
		seed(ctx, e, []export.Attachment{stored}, kept)
		in := inputOf(stored, incoming)
		stray := []byte("rs.status() during")
		straySHA := planAttachment(5, stray).SHA256
		in.sidecars = map[string][]byte{stored.SHA256: kept, incoming.SHA256: added, straySHA: stray}

		_, err := merge(ctx, s, in, mergeWrite)

		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(config.BlobDir(dbPath), straySHA)).NotTo(BeAnExistingFile())
		entries, err := os.ReadDir(config.BlobDir(dbPath))
		Expect(err).NotTo(HaveOccurred())
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			Expect(strings.HasPrefix(entry.Name(), ".tmp-")).To(BeFalse())
			names = append(names, entry.Name())
		}
		Expect(names).To(ConsistOf(stored.SHA256, incoming.SHA256))
	})

	It("serialises with another writer", func(ctx SpecContext) {
		other, err := store.Open(ctx, dbPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(other.Close)
		e := planEvent(7, tl.ID, "Pager fires")
		e.Source = new("pagerduty")
		in := inputOf(e)
		inside, release := make(chan struct{}), make(chan struct{})
		closeRelease := sync.OnceFunc(func() { close(release) })
		DeferCleanup(closeRelease)
		var once sync.Once
		in.newID = func() (uuid.UUID, error) {
			once.Do(func() { close(inside) })
			<-release
			return uuid.Parse(planID(100))
		}
		merged := make(chan error, 1)
		go func() {
			_, err := merge(ctx, s, in, mergeWrite)
			merged <- err
		}()
		Eventually(ctx, inside).Should(BeClosed())

		created := make(chan error, 1)
		go func() { created <- other.CreateTimeline(ctx, planTimeline(3, "inc-4822").Domain()) }()
		Consistently(created).WithTimeout(300 * time.Millisecond).ShouldNot(Receive())

		closeRelease()
		Eventually(ctx, merged).Should(Receive(BeNil()))
		Eventually(ctx, created).Should(Receive(Or(BeNil(), MatchError(store.ErrBusy))))
	}, SpecTimeout(time.Minute))
})
