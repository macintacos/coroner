package graphics_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/graphics"
)

func picture(width, height int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			m.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}

	return m
}

func encoded(encode func(*bytes.Buffer, image.Image) error, m image.Image) []byte {
	GinkgoHelper()
	var b bytes.Buffer
	Expect(encode(&b, m)).To(Succeed())
	return b.Bytes()
}

var _ = Describe("Decode", func() {
	It("keeps a PNG byte for byte, with its size in pixels", func() {
		content := encoded(func(b *bytes.Buffer, m image.Image) error { return png.Encode(b, m) }, picture(40, 30))

		img, err := graphics.Decode(content)

		Expect(err).NotTo(HaveOccurred())
		Expect(img.PNG).To(Equal(content))
		Expect([]int{img.Width, img.Height}).To(Equal([]int{40, 30}))
	})

	DescribeTable("re-encodes another format as PNG",
		func(encode func(*bytes.Buffer, image.Image) error) {
			img, err := graphics.Decode(encoded(encode, picture(40, 30)))

			Expect(err).NotTo(HaveOccurred())
			Expect([]int{img.Width, img.Height}).To(Equal([]int{40, 30}))
			decoded, format, err := image.Decode(bytes.NewReader(img.PNG))
			Expect(err).NotTo(HaveOccurred())
			Expect(format).To(Equal("png"))
			Expect(decoded.Bounds().Size()).To(Equal(image.Pt(40, 30)))
		},
		Entry("JPEG", func(b *bytes.Buffer, m image.Image) error { return jpeg.Encode(b, m, nil) }),
		Entry("GIF", func(b *bytes.Buffer, m image.Image) error { return gif.Encode(b, m, nil) }),
	)
})

var _ = Describe("DecodeSize", func() {
	DescribeTable("reads the size in pixels, without the PNG",
		func(encode func(*bytes.Buffer, image.Image) error) {
			size, err := graphics.DecodeSize(encoded(encode, picture(40, 30)))

			Expect(err).NotTo(HaveOccurred())
			Expect(size).To(Equal(graphics.Pixels{Width: 40, Height: 30}))
		},
		Entry("PNG", func(b *bytes.Buffer, m image.Image) error { return png.Encode(b, m) }),
		Entry("JPEG", func(b *bytes.Buffer, m image.Image) error { return jpeg.Encode(b, m, nil) }),
		Entry("GIF", func(b *bytes.Buffer, m image.Image) error { return gif.Encode(b, m, nil) }),
	)
})

// jpegHeader is a JFIF header claiming width×height pixels, with no image data behind it.
func jpegHeader(width, height uint16) func() []byte {
	return func() []byte {
		header := []byte{
			0xFF, 0xD8, // start of image
			0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
			0xFF, 0xC0, 0x00, 0x0B, 0x08, // baseline frame
		}
		header = binary.BigEndian.AppendUint16(header, height)
		header = binary.BigEndian.AppendUint16(header, width)

		return append(header, 0x01, 0x01, 0x11, 0x00) // one component
	}
}

func grayPNG(width, height int) func() []byte {
	return func() []byte {
		return encoded(func(b *bytes.Buffer, m image.Image) error { return png.Encode(b, m) },
			image.NewGray(image.Rect(0, 0, width, height)))
	}
}

var _ = DescribeTableSubtree("Decode and DecodeSize limits",
	func(decode func([]byte) error) {
		It("rejects content that is not a PNG, JPEG or GIF", func() {
			webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00")

			Expect(decode(webp)).To(MatchError(image.ErrFormat))
		})

		DescribeTable("rejects an image too large to decode safely or show, without decoding it",
			func(content func() []byte, size string) {
				err := decode(content())

				Expect(err).To(MatchError(ContainSubstring(size)))
			},
			Entry("a JPEG claiming more pixels than 64 million, under 10000 a side", jpegHeader(9000, 8000), "9000×8000"),
			Entry("a PNG wider than 10000 pixels", grayPNG(10_001, 1), "10001×1"),
			Entry("a PNG taller than 10000 pixels", grayPNG(1, 10_001), "1×10001"),
		)

		It("accepts an image 10000 pixels a side", func() {
			Expect(decode(grayPNG(10_000, 1)())).To(Succeed())
		})
	},
	Entry("Decode", func(content []byte) error { _, err := graphics.Decode(content); return err }),
	Entry("DecodeSize", func(content []byte) error { _, err := graphics.DecodeSize(content); return err }),
)

var _ = DescribeTable("IsImage",
	func(mime string, want bool) {
		Expect(graphics.IsImage(mime)).To(Equal(want))
	},
	Entry("an image Decode reads", "image/png", true),
	Entry("an image Decode cannot read", "image/webp", true),
	Entry("a media type in another case, with parameters", " Image/SVG+xml; charset=utf-8", true),
	Entry("text", "text/plain; charset=utf-8", false),
	Entry("no media type", "", false),
)

var _ = DescribeTable("Decodable",
	func(mime string, want bool) {
		Expect(graphics.Decodable(mime)).To(Equal(want))
	},
	Entry("PNG", "image/png", true),
	Entry("JPEG", "image/jpeg", true),
	Entry("GIF", "image/gif", true),
	Entry("a media type with parameters", "image/png; charset=binary", true),
	Entry("WebP, an image Decode cannot read", "image/webp", false),
	Entry("SVG", "image/svg+xml", false),
	Entry("text", "text/plain; charset=utf-8", false),
	Entry("no media type", "", false),
)
