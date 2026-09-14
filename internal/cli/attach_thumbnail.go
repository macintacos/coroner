package cli

import (
	"crypto/sha256"
	"encoding/binary"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/macintacos/coroner/internal/graphics"
)

// thumbnailCols and thumbnailRows are the most cells a form's attachment thumbnail takes.
const thumbnailCols, thumbnailRows = 24, 6

// attachThumbnail returns what an attach field shows under the file at a path: a PNG, JPEG or GIF
// through Kitty placeholder cells over its caption when images is Kitty, and its placeholder text
// otherwise, with the command that sends the terminal an image it has not been sent yet. A file
// it cannot read shows nothing, since the field's check reports it.
func attachThumbnail(readFile func(string) ([]byte, error), images graphics.Protocol) func(path string) (string, tea.Cmd) {
	sent := map[uint32]bool{}

	return func(path string) (string, tea.Cmd) {
		content, err := readFile(path)
		if err != nil {
			return "", nil
		}

		caption := graphics.Caption{Filename: filepath.Base(path), Size: int64(len(content))}
		img, err := graphics.Decode(content)
		if err == nil {
			caption.Image, caption.Width, caption.Height = true, img.Width, img.Height
		}
		if err != nil || images != graphics.Kitty {
			return caption.Placeholder(), nil
		}

		id, grid := imageID(content), graphics.Fit(img, graphics.Grid{Cols: thumbnailCols, Rows: thumbnailRows}, graphics.Pixels{})
		var send tea.Cmd
		if !sent[id] {
			sent[id] = true
			send = tea.Raw(graphics.Transmit(id, img.PNG) + graphics.Place(id, grid))
		}

		return strings.Join(append(graphics.Cells(id, grid), caption.String()), "\n"), send
	}
}

// imageID names content to the terminal by its hash, so a thumbnail left in the scrollback by an
// earlier form keeps its own image when another is sent.
func imageID(content []byte) uint32 {
	sum := sha256.Sum256(content)
	return max(binary.BigEndian.Uint32(sum[:4])&0xFFFFFF, 1)
}

// formProfile is the color profile a form draws in: none without color, and 24-bit color where
// images show, since an image's id travels as a color that 16 colors would round away.
func formProfile(color bool, lookupEnv func(string) (string, bool)) colorprofile.Profile {
	switch {
	case !color:
		return colorprofile.ASCII
	case graphics.Detect(lookupEnv) == graphics.Kitty:
		return colorprofile.TrueColor
	}

	return colorprofile.ANSI
}
