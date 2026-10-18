package graphics_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/graphics"
)

var _ = DescribeTable("Caption.Placeholder",
	func(c graphics.Caption, want string) {
		Expect(c.Placeholder()).To(Equal(want))
	},
	Entry("an image with its size in pixels",
		graphics.Caption{Filename: "p99.png", Size: 184_000, Width: 1280, Height: 720, Image: true},
		"[image: p99.png · 1280×720 · 184 KB]"),
	Entry("an image whose pixels are unknown",
		graphics.Caption{Filename: "p99.png", Size: 184_000, Image: true},
		"[image: p99.png · 184 KB]"),
	Entry("a file that is not an image",
		graphics.Caption{Filename: "trace.har", Size: 2_140_000},
		"[file: trace.har · 2 MB]"),
	Entry("a filename carrying an escape sequence",
		graphics.Caption{Filename: "p99\x1b[2J.png", Size: 10},
		"[file: p99.png · 10 B]"),
	Entry("a filename carrying control characters",
		graphics.Caption{Filename: "p99\a.png\n", Size: 10},
		"[file: p99.png · 10 B]"),
)

var _ = DescribeTable("ByteSize rounds a size into the largest unit it reaches, in steps of 1000",
	func(size int64, want string) {
		Expect(graphics.ByteSize(size)).To(Equal(want))
	},
	Entry("an empty file", int64(0), "0 B"),
	Entry("bytes", int64(999), "999 B"),
	Entry("1000 bytes is a kilobyte", int64(1000), "1 KB"),
	Entry("kilobytes", int64(184_320), "184 KB"),
	Entry("the largest size that stays in kilobytes", int64(999_499), "999 KB"),
	Entry("a size that rounds up to 1000 of a unit is the next unit", int64(999_500), "1 MB"),
	Entry("megabytes, with no decimals", int64(2_140_000), "2 MB"),
	Entry("a size that rounds up to a gigabyte", int64(999_950_000), "1 GB"),
	Entry("gigabytes, the largest unit", int64(4_200_000_000_000), "4200 GB"),
)
