package graphics

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// maxCells is how many rows or columns a placeholder can name: one per row-column diacritic.
const maxCells = 297

// Grid is a size in terminal cells.
type Grid struct {
	Cols, Rows int
}

// Pixels is a size in pixels.
type Pixels struct {
	Width, Height int
}

// ContentID names content to the terminal by its SHA-256: 24 bits, never 0.
func ContentID(content []byte) uint32 {
	sum := sha256.Sum256(content)
	return max(binary.BigEndian.Uint32(sum[:4])&0xFFFFFF, 1)
}

// Transmit returns the sequences that send png to the terminal as the image with id, without
// showing it. It asks the terminal for no reply (q=2), which the program would otherwise read as
// input. id must be in 1..(1<<24)-1.
func Transmit(id uint32, png []byte) string {
	payload := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	for start := 0; start < len(payload); start += kitty.MaxChunkSize {
		end := min(start+kitty.MaxChunkSize, len(payload))
		var opts []string
		if start == 0 {
			opts = []string{"a=t", "f=100", "t=d", "i=" + strconv.FormatUint(uint64(id), 10), "q=2"}
		}
		switch {
		case end < len(payload):
			opts = append(opts, "m=1")
		case start > 0:
			opts = append(opts, "m=0")
		}
		b.WriteString(ansi.KittyGraphics([]byte(payload[start:end]), opts...))
	}

	return b.String()
}

// Place returns the sequence that shows the image with id wherever Cells for it are drawn,
// scaled into grid. A later Place for the same image replaces it. id must be in 1..(1<<24)-1.
func Place(id uint32, grid Grid) string {
	return ansi.KittyGraphics(nil, "a=p", "U=1", "i="+strconv.FormatUint(uint64(id), 10), "p=1",
		"c="+strconv.Itoa(grid.Cols), "r="+strconv.Itoa(grid.Rows), "q=2")
}

// Cells returns grid's rows of placeholders showing the image with id, each naming its row and
// column, colored with the id, which the terminal reads back from the foreground color. id must be
// in 1..(1<<24)-1, the 24 bits the color carries, and grid is clamped to the 297 rows and columns a
// placeholder can name.
func Cells(id uint32, grid Grid) []string {
	grid = Grid{Cols: min(grid.Cols, maxCells), Rows: min(grid.Rows, maxCells)}
	color := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xFF, id>>8&0xFF, id&0xFF)
	rows := make([]string, grid.Rows)
	for r := range grid.Rows {
		var b strings.Builder
		b.WriteString(color)
		for c := range grid.Cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[39m")
		rows[r] = b.String()
	}

	return rows
}

// Fit returns the grid, within bounds, that shows img as wide as it can at its own aspect ratio in
// cells of cell pixels, and no larger than img's own size in those cells. A zero cell size counts
// as twice as tall as wide and leaves img's own size uncapped.
func Fit(img Image, bounds Grid, cell Pixels) Grid {
	cols := min(max(bounds.Cols, 1), maxCells)
	maxRows := min(max(bounds.Rows, 1), maxCells)
	if cell.Width <= 0 || cell.Height <= 0 {
		cell = Pixels{Width: 1, Height: 2}
	} else {
		cols = min(cols, max(ceilDiv(img.Width, cell.Width), 1))
		maxRows = min(maxRows, max(ceilDiv(img.Height, cell.Height), 1))
	}
	rows := max(ceilDiv(cols*cell.Width*img.Height, img.Width*cell.Height), 1)
	if rows > maxRows {
		rows = maxRows
		cols = min(cols, max(ceilDiv(rows*cell.Height*img.Width, img.Height*cell.Width), 1))
	}

	return Grid{Cols: cols, Rows: rows}
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}

	return (a + b - 1) / b
}
