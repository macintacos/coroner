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

	DescribeTable("sources and tags also allow : and /, timeline names do not",
		func(s string) {
			accepts(timeline.ValidateSourceName, s)
			accepts(timeline.ValidateTag, s)
			rejects(timeline.ValidateTimelineName, s)
		},
		Entry("namespaced tag", "phase:detect"),
		Entry("host and process", "db-2/mongod"),
	)
})
