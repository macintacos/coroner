package cli

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/export"
	"github.com/macintacos/coroner/internal/timeline"
)

var _ = DescribeTable("blobLink resolves a Markdown export's link to a blob from where the export is written",
	func(path, blob, want string) {
		Expect(blobLink(path, blob)).To(Equal(want))
	},
	Entry("from the file's directory when both paths are relative", "docs/rca.md", "blobs/abc", "../blobs/abc"),
	Entry("from a file in the working directory", "rca.md", "blobs/abc", "blobs/abc"),
	Entry("as the blob's path on stdout", "-", "blobs/abc", "blobs/abc"),
	Entry("as the blob's path when the file's is absolute", "/srv/rca.md", "blobs/abc", "blobs/abc"),
	Entry("as the blob's path when it is absolute", "docs/rca.md", "/srv/blobs/abc", "/srv/blobs/abc"),
)

var _ = Describe("newExport", func() {
	at := time.Date(2026, 9, 11, 3, 14, 7, 0, time.UTC)
	event := func(id string) timeline.Event {
		return timeline.Event{
			ID: id, TimelineID: "t1", Seq: 1, OccurredAt: at, Precision: timeline.PrecisionSecond, Tags: []string{},
			Text: "Primary stepped down", CreatedAt: at, UpdatedAt: at,
		}
	}
	link := func(id, from, to string) timeline.Link {
		return timeline.Link{ID: id, FromEventID: from, ToEventID: to, Kind: timeline.LinkLedTo, CreatedAt: at}
	}

	It("keeps a link only when it exports both the events the link joins", func() {
		env := newExport(exportInput{
			timelines: []timeline.Timeline{{ID: "t1", Name: "mongo-failover", Refs: []string{}}},
			events:    []timeline.Event{event("e1"), event("e2")},
			links:     []timeline.Link{link("l-out", "e1", "e9"), link("l-both", "e1", "e2"), link("l-in", "e9", "e2")},
		})

		Expect(env.Links).To(Equal([]export.Link{{ID: "l-both", FromEventID: "e1", ToEventID: "e2", Kind: timeline.LinkLedTo, CreatedAt: export.Instant(at)}}))
	})
})
