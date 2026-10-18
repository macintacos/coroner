package graphics

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	"image/png"
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

// Decodable reports whether Decode reads content of this media type: image/png, image/jpeg or
// image/gif, parameters ignored.
func Decodable(mime string) bool {
	mediaType, _, _ := strings.Cut(mime, ";")
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png", "image/jpeg", "image/gif":
		return true
	}

	return false
}

// Decode reads a PNG, JPEG or GIF, keeping a PNG as it is and re-encoding the others as PNG. It
// returns image.ErrFormat for any other content.
func Decode(content []byte) (Image, error) {
	config, format, err := decodeConfig(content)
	switch {
	case err != nil:
		return Image{}, err
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

// DecodeSize reads only the header of a PNG, JPEG or GIF: Image with Width and Height set and PNG
// nil, under the same format and size limits as Decode.
func DecodeSize(content []byte) (Image, error) {
	config, _, err := decodeConfig(content)
	if err != nil {
		return Image{}, err
	}

	return Image{Width: config.Width, Height: config.Height}, nil
}

func decodeConfig(content []byte) (image.Config, string, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	switch {
	case err != nil:
		return image.Config{}, "", err
	case config.Width > maxSide || config.Height > maxSide || config.Width*config.Height > maxPixels:
		return image.Config{}, "", fmt.Errorf("image of %d×%d pixels: larger than %d pixels a side or %d in all",
			config.Width, config.Height, maxSide, maxPixels)
	}

	return config, format, nil
}
