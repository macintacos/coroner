package cli

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("suggestName",
	func(input, want string) {
		Expect(suggestName(input)).To(Equal(want))
	},
	Entry("lowercases and dashes spaces", "Mongo Outage", "mongo-outage"),
	Entry("collapses a run of rejected characters into one dash", "db-2 / primary!!", "db-2-primary"),
	Entry("trims what a name cannot start or end with", "--x--", "x"),
	Entry("keeps dots and underscores", "INC_4821.retro", "inc_4821.retro"),
	Entry("replaces letters outside a-z", "ÉTÉ 2026", "t-2026"),
	Entry("cuts to 64 characters", strings.Repeat("A", 80), strings.Repeat("a", 64)),
	Entry("suggests nothing when no letter or digit survives", "!!", ""),
)
