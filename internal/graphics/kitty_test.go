package graphics_test

import (
	"bytes"
	"encoding/base64"

	"github.com/charmbracelet/x/ansi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/graphics"
)

var _ = Describe("Transmit", func() {
	It("sends a small image in one chunk", func() {
		Expect(graphics.Transmit(7, []byte("png"))).To(Equal("\x1b_Ga=t,f=100,t=d,i=7,q=2;cG5n\x1b\\"))
	})

	It("splits the base64 into 4096-byte chunks, marking all but the last as having more", func() {
		content := bytes.Repeat([]byte{0xAB}, 6000)
		encodedContent := base64.StdEncoding.EncodeToString(content)
		Expect(encodedContent).To(HaveLen(8000))

		Expect(graphics.Transmit(70000, content)).To(Equal(
			"\x1b_Ga=t,f=100,t=d,i=70000,q=2,m=1;" + encodedContent[:4096] + "\x1b\\" +
				"\x1b_Gm=0;" + encodedContent[4096:] + "\x1b\\",
		))
	})
})

var _ = Describe("Place", func() {
	It("puts one virtual placement, which a later Place for the image replaces", func() {
		Expect(graphics.Place(7, graphics.Grid{Cols: 40, Rows: 11})).To(Equal("\x1b_Ga=p,U=1,i=7,p=1,c=40,r=11,q=2\x1b\\"))
	})
})

var _ = Describe("Delete", func() {
	It("frees the image and its placements", func() {
		Expect(graphics.Delete(7)).To(Equal("\x1b_Ga=d,d=I,i=7,q=2\x1b\\"))
	})
})

var _ = Describe("Cells", func() {
	It("fills each row with placeholders naming their row and column, colored with the image id", func() {
		// 70000 is 0x011170: red 1, green 17, blue 112.
		Expect(graphics.Cells(70000, graphics.Grid{Cols: 2, Rows: 2})).To(Equal([]string{
			"\x1b[38;2;1;17;112m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m",
			"\x1b[38;2;1;17;112m\U0010EEEE̍̅\U0010EEEE̍̍\x1b[39m",
		}))
	})

	It("takes one cell per placeholder", func() {
		for _, row := range graphics.Cells(3, graphics.Grid{Cols: 37, Rows: 4}) {
			Expect(ansi.StringWidth(row)).To(Equal(37))
		}
	})
})

var _ = DescribeTable("Fit",
	func(bounds graphics.Grid, cell graphics.Pixels, want graphics.Grid) {
		img := graphics.Image{Width: 400, Height: 225}
		Expect(graphics.Fit(img, bounds, cell)).To(Equal(want))
	},
	Entry("the full width, rows rounded up to keep the aspect ratio",
		graphics.Grid{Cols: 40, Rows: 100}, graphics.Pixels{Width: 19, Height: 39}, graphics.Grid{Cols: 40, Rows: 11}),
	Entry("fewer rows than the width needs, narrowing the columns to match",
		graphics.Grid{Cols: 40, Rows: 5}, graphics.Pixels{Width: 19, Height: 39}, graphics.Grid{Cols: 19, Rows: 5}),
	Entry("an unknown cell size, taken as twice as tall as wide",
		graphics.Grid{Cols: 40, Rows: 100}, graphics.Pixels{}, graphics.Grid{Cols: 40, Rows: 12}),
	Entry("a width beyond the 297 cells a placeholder can name",
		graphics.Grid{Cols: 500, Rows: 1000}, graphics.Pixels{Width: 10, Height: 20}, graphics.Grid{Cols: 297, Rows: 84}),
	Entry("no room at all, still one cell",
		graphics.Grid{Cols: 0, Rows: 0}, graphics.Pixels{Width: 10, Height: 20}, graphics.Grid{Cols: 1, Rows: 1}),
)

var _ = Describe("Fit on a tall, narrow image", func() {
	It("keeps at least one column", func() {
		img := graphics.Image{Width: 1, Height: 1000}
		Expect(graphics.Fit(img, graphics.Grid{Cols: 40, Rows: 3}, graphics.Pixels{Width: 10, Height: 20})).
			To(Equal(graphics.Grid{Cols: 1, Rows: 3}))
	})
})
