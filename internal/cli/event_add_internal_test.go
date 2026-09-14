package cli

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

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
