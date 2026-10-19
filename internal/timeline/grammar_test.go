package timeline_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("Grammar", func() {
	type validator func(string) error

	accepts := func(validate validator, s string) {
		Expect(validate(s)).To(Succeed())
	}
	rejects := func(validate validator, s string) {
		Expect(validate(s)).NotTo(Succeed())
	}

	for _, v := range []struct {
		name     string
		validate validator
	}{
		{"ValidateTimelineName", timeline.ValidateTimelineName},
		{"ValidateOverlayName", timeline.ValidateOverlayName},
		{"ValidateSourceName", timeline.ValidateSourceName},
		{"ValidateTag", timeline.ValidateTag},
	} {
		name, validate := v.name, v.validate

		DescribeTable(name+" accepts lowercase slugs up to 64 characters",
			func(s string) { accepts(validate, s) },
			Entry("one character", "a"),
			Entry("leading digit", "465"),
			Entry("dash", "inc-465"),
			Entry("dot and underscore", "db.2_x"),
			Entry("64 characters", strings.Repeat("a", 64)),
		)

		DescribeTable(name+" rejects anything else",
			func(s string) { rejects(validate, s) },
			Entry("empty", ""),
			Entry("leading dash", "-a"),
			Entry("leading dot", ".a"),
			Entry("uppercase", "Inc"),
			Entry("space", "a b"),
			Entry("non-ASCII", "é"),
			Entry("65 characters", strings.Repeat("a", 65)),
		)
	}

	DescribeTable("sources and tags also allow : and /, timeline and overlay names do not",
		func(s string) {
			accepts(timeline.ValidateSourceName, s)
			accepts(timeline.ValidateTag, s)
			rejects(timeline.ValidateTimelineName, s)
			rejects(timeline.ValidateOverlayName, s)
		},
		Entry("namespaced tag", "phase:detect"),
		Entry("host and process", "db-2/mongod"),
	)

	It("reserves none as a source name, for clearing an event's source", func() {
		rejects(timeline.ValidateSourceName, "none")
		accepts(timeline.ValidateTag, "none")
		accepts(timeline.ValidateTimelineName, "none")
	})
})

var _ = Describe("Suggest", func() {
	DescribeTable("SuggestTimelineName",
		func(input, want string) {
			Expect(timeline.SuggestTimelineName(input)).To(Equal(want))
		},
		Entry("lowercases and dashes spaces", "Mongo Outage", "mongo-outage"),
		Entry("collapses a run of rejected characters into one dash", "db-2 / primary!!", "db-2-primary"),
		Entry("trims what a name cannot start or end with", "--x--", "x"),
		Entry("keeps dots and underscores", "INC_4821.retro", "inc_4821.retro"),
		Entry("replaces letters outside a-z", "ÉTÉ 2026", "t-2026"),
		Entry("cuts to 64 characters", strings.Repeat("A", 80), strings.Repeat("a", 64)),
		Entry("suggests nothing when no letter or digit survives", "!!", ""),
	)

	It("SuggestOverlayName follows the timeline name grammar", func() {
		Expect(timeline.SuggestOverlayName("DB-2/Primary")).To(Equal("db-2-primary"))
	})

	DescribeTable("SuggestSourceName and SuggestTag keep the colons and slashes their grammar allows",
		func(input, want string) {
			Expect(timeline.SuggestSourceName(input)).To(Equal(want))
			Expect(timeline.SuggestTag(input)).To(Equal(want))
		},
		Entry("keeps a slash", "DB-2/Mongod Primary", "db-2/mongod-primary"),
		Entry("trims a leading colon", ":Phase:Detect", "phase:detect"),
	)

	It("SuggestSourceName suggests nothing for the reserved none", func() {
		Expect(timeline.SuggestSourceName("NONE")).To(BeEmpty())
		Expect(timeline.SuggestTag("NONE")).To(Equal("none"))
	})
})
