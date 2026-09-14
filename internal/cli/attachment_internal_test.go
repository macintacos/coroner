package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("formatSize rounds a size into the largest unit it reaches, in steps of 1000",
	func(size int64, want string) {
		Expect(formatSize(size)).To(Equal(want))
	},
	Entry("bytes", int64(999), "999 B"),
	Entry("1000 bytes is a kilobyte", int64(1000), "1 KB"),
	Entry("kilobytes", int64(184_320), "184 KB"),
	Entry("megabytes", int64(3_400_000), "3 MB"),
	Entry("gigabytes, the largest unit", int64(4_200_000_000_000), "4200 GB"),
)
