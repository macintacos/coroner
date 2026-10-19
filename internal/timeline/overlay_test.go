package timeline_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Overlay.Validate", func() {
	It("rejects a name outside the grammar", func() {
		o := timeline.Overlay{ID: "0199a1b2-0000-7000-8000-000000000003", Name: "db outage", CreatedAt: created()}

		Expect(fieldRules(o.Validate())).To(Equal([]timeline.Problem{{Field: "name", Rule: timeline.RuleGrammar}}))
	})
})
