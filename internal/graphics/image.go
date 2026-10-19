package graphics

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
)

// maxPixels and maxSide bound the images Decode accepts: a small file can claim an image too large
// to decode, and Kitty and Ghostty reject a side longer than maxSide.
const (
	maxPixels = 64_000_000
	maxSide   = 10_000
)

// Image is an image ready for the Kitty protocol.
type Image struct {
	PNG []byte
	// Width and Height are its size in pixels.
	Width, Height int
}

// format is a decoder for one of the formats Decode reads.
type format struct {
	name         string
	decodeConfig func(io.Reader) (image.Config, error)
	decode       func(io.Reader) (image.Image, error)
}

// sniff returns the format content's magic bytes name, and false for one Decode does not read. The
// decoders are called directly rather than through image.Decode, so no other format a binary
// registers is read.
func sniff(content []byte) (format, bool) {
	switch {
	case bytes.HasPrefix(content, []byte("\x89PNG\r\n\x1a\n")):
		return format{"png", png.DecodeConfig, png.Decode}, true
	case bytes.HasPrefix(content, []byte("\xff\xd8")):
		return format{"jpeg", jpeg.DecodeConfig, jpeg.Decode}, true
	case bytes.HasPrefix(content, []byte("GIF8")):
		return format{"gif", gif.DecodeConfig, gif.Decode}, true
	}

	return format{}, false
}

// mediaType is mime's media type, lower-cased and without parameters.
func mediaType(mime string) string {
	t, _, _ := strings.Cut(mime, ";")
	return strings.ToLower(strings.TrimSpace(t))
}

// IsImage reports whether mime names an image of any format, whether or not Decode reads it.
func IsImage(mime string) bool {
	return strings.HasPrefix(mediaType(mime), "image/")
}

// Decodable reports whether Decode reads content of this media type: image/png, image/jpeg or
// image/gif, parameters ignored.
func Decodable(mime string) bool {
	switch mediaType(mime) {
	case "image/png", "image/jpeg", "image/gif":
		return true
	}

	return false
}

// Decode reads a PNG, JPEG or GIF, keeping a PNG as it is and re-encoding the others as PNG. It
// returns image.ErrFormat for any other content.
func Decode(content []byte) (Image, error) {
	f, config, err := decodeConfig(content)
	switch {
	case err != nil:
		return Image{}, err
	case f.name == "png":
		return Image{PNG: content, Width: config.Width, Height: config.Height}, nil
	}

	decoded, err := f.decode(bytes.NewReader(content))
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

// DecodeSize reads only the header of a PNG, JPEG or GIF and returns its size, under the same
// format and size limits as Decode.
func DecodeSize(content []byte) (Pixels, error) {
	_, config, err := decodeConfig(content)
	if err != nil {
		return Pixels{}, err
	}

	return Pixels{Width: config.Width, Height: config.Height}, nil
}

func decodeConfig(content []byte) (format, image.Config, error) {
	f, ok := sniff(content)
	if !ok {
		return format{}, image.Config{}, image.ErrFormat
	}

	config, err := f.decodeConfig(bytes.NewReader(content))
	switch {
	case err != nil:
		return format{}, image.Config{}, err
	case config.Width > maxSide || config.Height > maxSide || config.Width*config.Height > maxPixels:
		return format{}, image.Config{}, fmt.Errorf("image of %d×%d pixels: larger than %d pixels a side or %d in all",
			config.Width, config.Height, maxSide, maxPixels)
	}

	return f, config, nil
}
