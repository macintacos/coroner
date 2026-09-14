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
		"[file: trace.har · 2.1 MB]"),
	Entry("a filename carrying control characters",
		graphics.Caption{Filename: "p99\x1b[2J.png\n", Size: 10},
		"[file: p99[2J.png · 10 B]"),
)

var _ = DescribeTable("Caption.String sizes",
	func(size int64, want string) {
		Expect(graphics.Caption{Filename: "f", Size: size}.String()).To(Equal("f · " + want))
	},
	Entry("an empty file", int64(0), "0 B"),
	Entry("the largest size in bytes", int64(999), "999 B"),
	Entry("one kilobyte", int64(1000), "1 KB"),
	Entry("kilobytes, rounded", int64(184_499), "184 KB"),
	Entry("the size that rounds up to a megabyte", int64(999_500), "1.0 MB"),
	Entry("megabytes to one decimal", int64(2_140_000), "2.1 MB"),
	Entry("the size that rounds up to a gigabyte", int64(999_950_000), "1.0 GB"),
)
