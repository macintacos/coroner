package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("closest returns the candidate the fewest edits away, the first on a tie",
	func(name string, candidates []string, want string) {
		Expect(closest(name, candidates)).To(Equal(want))
	},
	Entry("transposed letters", "tmie", []string{"environment", "time", "version"}, "time"),
	Entry("a missing letter", "envronment", []string{"time", "environment"}, "environment"),
	Entry("an extra letter", "versions", []string{"version", "help"}, "version"),
	Entry("a name close to nothing", "qqqq", []string{"environment", "help"}, "help"),
	Entry("a tie", "ab", []string{"ax", "xb"}, "ax"),
)
