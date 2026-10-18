package timeline_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Link.Validate", func() {
	valid := func() timeline.Link {
		return timeline.Link{
			ID:          "0199a1b2-0000-7000-8000-000000000003",
			FromEventID: "0199a1b2-0000-7000-8000-000000000001",
			ToEventID:   "0199a1b2-0000-7000-8000-000000000002",
			Kind:        timeline.LinkLedTo,
			CreatedAt:   created(),
		}
	}

	DescribeTable("accepts every kind",
		func(kind timeline.LinkKind) {
			l := valid()
			l.Kind = kind
			Expect(l.Validate()).To(Succeed())
		},
		Entry("led_to", timeline.LinkLedTo),
		Entry("same_as", timeline.LinkSameAs),
		Entry("copied_from", timeline.LinkCopiedFrom),
	)

	DescribeTable("rejects",
		func(rule string, mutate func(*timeline.Link)) {
			l := valid()
			mutate(&l)
			Expect(l.Validate()).To(MatchError(HavePrefix(rule)))
		},
		Entry("an unknown kind", "link kind", func(l *timeline.Link) { l.Kind = "after" }),
		Entry("a link from an event to itself", "link", func(l *timeline.Link) { l.ToEventID = l.FromEventID }),
	)
})
