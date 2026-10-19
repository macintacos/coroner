package cli

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/timeline"
)

var _ = Describe("suggestSlug", func() {
	DescribeTable("makes a timeline name",
		func(input, want string) {
			Expect(suggestSlug(input, namePunct, timeline.ValidateTimelineName)).To(Equal(want))
		},
		Entry("lowercases and dashes spaces", "Mongo Outage", "mongo-outage"),
		Entry("collapses a run of rejected characters into one dash", "db-2 / primary!!", "db-2-primary"),
		Entry("trims what a name cannot start or end with", "--x--", "x"),
		Entry("keeps dots and underscores", "INC_4821.retro", "inc_4821.retro"),
		Entry("replaces letters outside a-z", "ÉTÉ 2026", "t-2026"),
		Entry("cuts to 64 characters", strings.Repeat("A", 80), strings.Repeat("a", 64)),
		Entry("suggests nothing when no letter or digit survives", "!!", ""),
	)

	DescribeTable("makes a source or tag, keeping the colons and slashes its grammar allows",
		func(input, want string) {
			Expect(suggestSlug(input, sourceTagPunct, timeline.ValidateSourceName)).To(Equal(want))
		},
		Entry("keeps a slash", "DB-2/Mongod Primary", "db-2/mongod-primary"),
		Entry("trims a leading colon", ":Phase:Detect", "phase:detect"),
	)
})

var _ = Describe("the picker's shadowing messages", func() {
	picked := timeline.Timeline{ID: "0199a1b2-0000-7000-8000-000000000001", Name: "mongo-outage"}
	envWith := func(env map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		}
	}

	DescribeTable("a notice names CORONER_TIMELINE when it selects a timeline",
		func(env map[string]string, wantNotice bool) {
			notice := shadowNotice(envWith(env))

			if wantNotice {
				Expect(notice).To(ContainSubstring(`CORONER_TIMELINE="db-2"`))
			} else {
				Expect(notice).To(BeEmpty())
			}
		},
		Entry("names the variable and its value", map[string]string{"CORONER_TIMELINE": "db-2"}, true),
		Entry("says nothing without the variable", nil, false),
		Entry("says nothing with an empty variable", map[string]string{"CORONER_TIMELINE": ""}, false),
	)

	DescribeTable("a warning after a pick names CORONER_TIMELINE when it selects another timeline",
		func(env map[string]string, wantWarning bool) {
			warning := shadowWarning(envWith(env), picked)

			if wantWarning {
				Expect(warning).To(ContainSubstring(`CORONER_TIMELINE="db-2"`))
				Expect(warning).To(ContainSubstring("mongo-outage"))
			} else {
				Expect(warning).To(BeEmpty())
			}
		},
		Entry("warns when the variable names another timeline", map[string]string{"CORONER_TIMELINE": "db-2"}, true),
		Entry("stays quiet without the variable", nil, false),
		Entry("stays quiet with an empty variable", map[string]string{"CORONER_TIMELINE": ""}, false),
		Entry("stays quiet when the variable names the picked timeline", map[string]string{"CORONER_TIMELINE": "mongo-outage"}, false),
		Entry("stays quiet when the variable holds the picked timeline's id",
			map[string]string{"CORONER_TIMELINE": "0199a1b2-0000-7000-8000-000000000001"}, false),
	)
})
