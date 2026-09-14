package cli

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/store"
)

// thumbnailCols and thumbnailRows are the most cells a form's attachment thumbnail takes.
const thumbnailCols, thumbnailRows = 24, 6

// attachThumbnail returns what an attach field shows under the file at a path, reading each file
// once: its thumbnail, and the command that sends the terminal its image the first time it shows.
// A path that cannot be read shows no thumbnail.
func attachThumbnail(readFile func(string) ([]byte, error), images graphics.Protocol) func(path string) (string, tea.Cmd) {
	shown := map[string]string{}

	return func(path string) (string, tea.Cmd) {
		if lines, ok := shown[path]; ok {
			return lines, nil
		}
		// A failed read stays out of shown, so a file created after it is typed still shows.
		content, err := readFile(path)
		if err != nil {
			return "", nil
		}

		lines, send := thumbnail(content, filepath.Base(path), images)
		shown[path] = lines

		return lines, send
	}
}

// thumbnail returns the lines that show content, named filename, and the command that sends its
// image: a PNG, JPEG or GIF through Kitty placeholder cells over its caption when images is Kitty,
// and its placeholder text otherwise, captioned as the attachment will be stored.
func thumbnail(content []byte, filename string, images graphics.Protocol) (string, tea.Cmd) {
	mime := store.DetectMIME(content, filename)
	caption := graphics.Caption{Filename: filename, Size: int64(len(content)), Image: strings.HasPrefix(mime, "image/")}
	if !graphics.Decodable(mime) {
		return caption.Placeholder(), nil
	}
	if images != graphics.Kitty {
		size, err := graphics.DecodeSize(content)
		if err == nil {
			caption.Width, caption.Height = size.Width, size.Height
		}
		return caption.Placeholder(), nil
	}

	img, err := graphics.Decode(content)
	if err != nil {
		return caption.Placeholder(), nil
	}
	caption.Width, caption.Height = img.Width, img.Height
	id, grid := graphics.ContentID(content), graphics.Fit(img, graphics.Grid{Cols: thumbnailCols, Rows: thumbnailRows}, graphics.Pixels{})

	return strings.Join(append(graphics.Cells(id, grid), caption.String()), "\n"), tea.Raw(graphics.Transmit(id, img.PNG) + graphics.Place(id, grid))
}

// formProfile is the color profile a form draws in: none without color, and 24-bit color when an
// attach field shows images, since an image's id travels as a color that 16 colors would round away.
func formProfile(color bool, images graphics.Protocol, attach bool) colorprofile.Profile {
	switch {
	case !color:
		return colorprofile.ASCII
	case images == graphics.Kitty && attach:
		return colorprofile.TrueColor
	}

	return colorprofile.ANSI
}
