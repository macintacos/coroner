package graphics

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Caption describes an attachment in words.
type Caption struct {
	Filename string
	// Size is the content's length in bytes.
	Size int64
	// Width and Height are the image's size in pixels, zero when unknown.
	Width, Height int
	// Image makes the placeholder read [image: …] rather than [file: …].
	Image bool
}

// String is the filename, the width×height when known, and the size in bytes, as in
// "p99.png · 1280×720 · 184 KB".
func (c Caption) String() string {
	parts := []string{strings.Map(dropControl, ansi.Strip(c.Filename))}
	if c.Width > 0 && c.Height > 0 {
		parts = append(parts, fmt.Sprintf("%d×%d", c.Width, c.Height))
	}

	return strings.Join(append(parts, ByteSize(c.Size)), " · ")
}

// Placeholder is the text shown where the attachment cannot, as in
// "[image: p99.png · 1280×720 · 184 KB]" or "[file: trace.har · 2 MB]".
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

// ByteSize renders n in the largest of B, KB, MB and GB whose rounded value stays under 1000, with
// no decimals, as in "2 MB", "184 KB", "999 B" or "1 GB".
func ByteSize(n int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	value, unit := float64(n), 0
	for math.Round(value) >= 1000 && unit < len(units)-1 {
		value /= 1000
		unit++
	}

	return fmt.Sprintf("%.0f %s", value, units[unit])
}
