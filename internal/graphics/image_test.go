package graphics_test

import (
	"bytes"
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

	It("rejects content that is not a PNG, JPEG or GIF", func() {
		webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00")

		_, err := graphics.Decode(webp)

		Expect(err).To(MatchError(image.ErrFormat))
	})

	It("rejects a JPEG too large to decode safely, without decoding it", func() {
		// A JFIF header claiming 65535×65535 pixels, with no image data behind it.
		header := []byte{
			0xFF, 0xD8, // start of image
			0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
			0xFF, 0xC0, 0x00, 0x0B, 0x08, 0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x01, 0x11, 0x00, // baseline frame, one component
		}

		_, err := graphics.Decode(header)

		Expect(err).To(MatchError(ContainSubstring("65535×65535")))
	})
})
