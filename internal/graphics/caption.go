package graphics

import (
	"fmt"
	"strings"
	"unicode"
)

// Caption describes an attachment in words.
type Caption struct {
	Filename string
	// Size is the content's length in bytes.
	Size int64
	// Width and Height are the image's size in pixels, zero when unknown.
	Width, Height int
	Image         bool
}

// String is the filename, the size in pixels when known, and the size, as in
// "p99.png · 1280×720 · 184 KB".
func (c Caption) String() string {
	parts := []string{strings.Map(dropControl, c.Filename)}
	if c.Width > 0 && c.Height > 0 {
		parts = append(parts, fmt.Sprintf("%d×%d", c.Width, c.Height))
	}

	return strings.Join(append(parts, byteSize(c.Size)), " · ")
}

// Placeholder is the text shown where the attachment cannot, as in
// "[image: p99.png · 1280×720 · 184 KB]" or "[file: trace.har · 2.1 MB]".
func (c Caption) Placeholder() string {
	kind := "file"
	if c.Image {
		kind = "image"
	}

	return "[" + kind + ": " + c.String() + "]"
}

func dropControl(r rune) rune {
	if unicode.IsControl(r) {
		return -1
	}

	return r
}

// byteSize renders n in decimal units: whole bytes and kilobytes, then megabytes and gigabytes
// to one decimal.
func byteSize(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 999_500:
		return fmt.Sprintf("%.0f KB", float64(n)/1e3)
	case n < 999_950_000:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}

	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}
