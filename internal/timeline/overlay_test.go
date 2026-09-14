package timeline_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Overlay.Validate", func() {
	valid := func() timeline.Overlay {
		return timeline.Overlay{
			ID:          "0199a1b2-0000-7000-8000-000000000004",
			Name:        "checkout-incident",
			TimelineIDs: []string{"0199a1b2-0000-7000-8000-000000000000", "0199a1b2-0000-7000-8000-000000000001"},
			CreatedAt:   created(),
		}
	}

	It("accepts a named set of timelines", func() {
		Expect(valid().Validate()).To(Succeed())
	})

	DescribeTable("rejects",
		func(rule string, mutate func(*timeline.Overlay)) {
			o := valid()
			mutate(&o)
			Expect(o.Validate()).To(MatchError(HavePrefix(rule)))
		},
		Entry("a name outside the grammar", "overlay name", func(o *timeline.Overlay) { o.Name = "Checkout Incident" }),
		Entry("no timelines", "timelines", func(o *timeline.Overlay) { o.TimelineIDs = nil }),
	)
})
