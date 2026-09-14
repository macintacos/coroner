package graphics

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	"image/png"
)

// maxPixels bounds what Decode decodes, since a small file can claim a huge image.
const maxPixels = 64_000_000

// Image is an image ready for the Kitty protocol.
type Image struct {
	PNG []byte
	// Width and Height are its size in pixels.
	Width, Height int
}

// Decode reads a PNG, JPEG or GIF, keeping a PNG as it is and re-encoding the others as PNG. It
// returns image.ErrFormat for any other content.
func Decode(content []byte) (Image, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	switch {
	case err != nil:
		return Image{}, err
	case config.Width*config.Height > maxPixels:
		return Image{}, fmt.Errorf("image of %d×%d pixels: larger than %d pixels", config.Width, config.Height, maxPixels)
	case format == "png":
		return Image{PNG: content, Width: config.Width, Height: config.Height}, nil
	}

	decoded, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return Image{}, err
	}
	var b bytes.Buffer
	err = png.Encode(&b, decoded)
	if err != nil {
		return Image{}, err
	}

	return Image{PNG: b.Bytes(), Width: config.Width, Height: config.Height}, nil
}
