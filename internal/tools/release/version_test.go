package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("nextTag", func() {
	DescribeTable("bumps the latest final tag",
		func(tags []string, bump string, rc bool, want string) {
			next, err := parseBump(bump)
			Expect(err).NotTo(HaveOccurred())
			Expect(nextTag(tags, next, rc)).To(Equal(want))
		},
		Entry("no tags, minor rc", nil, "minor", true, "v0.1.0-rc.0"),
		Entry("no tags, patch", nil, "patch", false, "v0.0.1"),
		Entry("no tags, major", nil, "major", false, "v1.0.0"),
		Entry("only rcs, minor", []string{"v0.1.0-rc.0", "v0.1.0-rc.1"}, "minor", false, "v0.1.0"),
		Entry("only rcs, minor rc", []string{"v0.1.0-rc.0", "v0.1.0-rc.1"}, "minor", true, "v0.1.0-rc.2"),
		Entry("mixed, minor", []string{"v0.1.0", "v0.2.0-rc.0", "v0.1.1-rc.3"}, "minor", false, "v0.2.0"),
		Entry("mixed, minor rc", []string{"v0.1.0", "v0.2.0-rc.0", "v0.1.1-rc.3"}, "minor", true, "v0.2.0-rc.1"),
		Entry("mixed, patch rc", []string{"v0.1.0", "v0.2.0-rc.0", "v0.1.1-rc.3"}, "patch", true, "v0.1.1-rc.4"),
		Entry("major resets minor and patch", []string{"v1.4.7"}, "major", false, "v2.0.0"),
		Entry("compares numerically", []string{"v0.9.0", "v0.10.0"}, "minor", false, "v0.11.0"),
		Entry("ignores malformed tags", []string{"v1.2", "v1.2.3-beta.1", "v0.3.0"}, "patch", false, "v0.3.1"),
	)

	DescribeTable("parseTag rejects a number with a leading zero",
		func(tag string) {
			_, ok := parseTag(tag)
			Expect(ok).To(BeFalse())
		},
		Entry("major", "v01.2.3"),
		Entry("patch", "v1.2.03"),
		Entry("rc", "v1.2.3-rc.01"),
	)

	It("rejects an unknown bump", func() {
		_, err := parseBump("bogus")
		Expect(err).To(HaveOccurred())
	})
})
