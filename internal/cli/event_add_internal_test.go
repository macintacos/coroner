package cli

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("event add --id", func() {
	type ran struct {
		code        int
		out, errOut string
	}

	It("exits 0 and prints the id for every copy of one command run at once", func(ctx SpecContext) {
		home := GinkgoT().TempDir()
		db := filepath.Join(home, "coroner.db")
		run := func(args ...string) ran {
			var out, errOut bytes.Buffer
			code := Run(ctx, App{
				Args: append([]string{"--db", db}, args...), In: strings.NewReader(""), Out: &out, Err: &errOut,
				LookupEnv: func(key string) (string, bool) {
					value, ok := map[string]string{"HOME": home}[key]
					return value, ok
				},
				ReadFile:  func(string) ([]byte, error) { return nil, fs.ErrNotExist },
				Now:       func() time.Time { return time.Date(2026, 9, 13, 1, 30, 0, 0, time.UTC) },
				NewID:     uuid.NewV7,
				OpenStore: store.Open,
			})
			return ran{code: code, out: out.String(), errOut: errOut.String()}
		}
		created := run("timeline", "create", "mongo-outage")
		Expect(created.code).To(Equal(0), created.errOut)

		ids := make([]string, 40)
		results := make([][3]ran, len(ids))
		var wg sync.WaitGroup
		for i := range ids {
			id, err := uuid.NewV7()
			Expect(err).NotTo(HaveOccurred())
			ids[i] = id.String()
			for c := range results[i] {
				wg.Go(func() {
					results[i][c] = run("event", "add", "Election timeout on db-3", "-t", "mongo-outage",
						"--id", ids[i], "--at", "2026-09-11T03:14:30Z", "-q")
				})
			}
		}
		wg.Wait()

		for i, id := range ids {
			want := ran{code: 0, out: id + "\n"}
			Expect(results[i]).To(Equal([3]ran{want, want, want}))
		}
	})
})

var _ = Describe("changedEventKeys", func() {
	stored := func() timeline.Event {
		at := time.Date(2026, 9, 11, 3, 14, 30, 0, time.UTC)
		return timeline.Event{
			ID: "01a0a07c-0000-7000-8000-000000000001", TimelineID: "01a09862-b1c0-7000-8000-000000000001", Seq: 3,
			OccurredAt: at, Precision: timeline.PrecisionSecond, Source: new("mongod"), Tags: []string{"election", "phase:detect"},
			Text: "Election timeout on db-3", CreatedAt: at, UpdatedAt: at,
		}
	}

	DescribeTable("names the keys a re-add under the same id would change",
		func(change func(e *timeline.Event), want []string) {
			draft := stored()
			change(&draft)

			Expect(changedEventKeys(stored(), draft)).To(Equal(want))
		},
		Entry("none when only what the store sets differs: seq, created and updated at", func(e *timeline.Event) {
			e.Seq = 0
			e.CreatedAt = e.CreatedAt.Add(time.Hour)
			e.UpdatedAt = e.UpdatedAt.Add(time.Hour)
		}, nil),
		Entry("none when tags come in another order", func(e *timeline.Event) {
			e.Tags = []string{"phase:detect", "election"}
		}, nil),
		Entry("tags, when the re-add has none", func(e *timeline.Event) { e.Tags = []string{} }, []string{"tags"}),
		Entry("text", func(e *timeline.Event) { e.Text = "Election timeout on db-4" }, []string{"text"}),
		Entry("the timeline", func(e *timeline.Event) { e.TimelineID = "01a09862-b1c0-7000-8000-000000000009" }, []string{"timeline_id"}),
		Entry("several keys, in the export's key order", func(e *timeline.Event) {
			e.Source = nil
			e.OccurredAt = e.OccurredAt.Add(time.Second)
		}, []string{"occurred_at", "source"}),
	)
})
