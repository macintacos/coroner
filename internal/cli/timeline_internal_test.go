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

var _ = DescribeTable("resolveTimeline",
	func(flag *string, env map[string]string, stored, wantNameOrID string, wantBy selectedBy) {
		lookupEnv := func(key string) (string, bool) {
			value, ok := env[key]
			return value, ok
		}

		nameOrID, by := resolveTimeline(flag, lookupEnv, stored)

		Expect(nameOrID).To(Equal(wantNameOrID))
		Expect(by).To(Equal(wantBy))
	},
	Entry("selects nothing without a flag, variable or stored timeline", nil, nil, "", "", selectedBy("")),
	Entry("selects nothing with only an empty variable", nil, map[string]string{"CORONER_TIMELINE": ""}, "", "", selectedBy("")),
	Entry("selects the variable's timeline", nil, map[string]string{"CORONER_TIMELINE": "b"}, "", "b", selectedByEnv),
	Entry("selects the stored timeline alone", nil, nil, "id-c", "id-c", selectedByUse),
	Entry("selects the stored timeline over an empty variable",
		nil, map[string]string{"CORONER_TIMELINE": ""}, "id-c", "id-c", selectedByUse),
	Entry("selects the variable's timeline over the stored one",
		nil, map[string]string{"CORONER_TIMELINE": "b"}, "id-c", "b", selectedByEnv),
	Entry("selects the flag's timeline alone", new("a"), nil, "", "a", selectedByFlag),
	Entry("selects the flag's timeline over an empty variable",
		new("a"), map[string]string{"CORONER_TIMELINE": ""}, "", "a", selectedByFlag),
	Entry("selects the flag's timeline over the variable's",
		new("a"), map[string]string{"CORONER_TIMELINE": "b"}, "", "a", selectedByFlag),
	Entry("selects the flag's timeline over the stored one", new("a"), nil, "id-c", "a", selectedByFlag),
	Entry("selects the flag's timeline over an empty variable and the stored one",
		new("a"), map[string]string{"CORONER_TIMELINE": ""}, "id-c", "a", selectedByFlag),
	Entry("selects the flag's timeline over the variable's and the stored one",
		new("a"), map[string]string{"CORONER_TIMELINE": "b"}, "id-c", "a", selectedByFlag),
)
