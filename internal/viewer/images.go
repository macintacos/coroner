package viewer

import (
	"maps"
	"strings"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/timeline"
)

type imageState int

const (
	imageReading imageState = iota
	imageFailed
	imageRead
	imageSent
)

// shownImage is an image attachment's state, its size in pixels once read, and once sent the id
// naming it on the terminal and the grid it is placed in.
type shownImage struct {
	state imageState
	img   graphics.Image
	id    uint32
	grid  graphics.Grid
}

// inlineImages is how the terminal shows images, and what the viewer has read and sent of each
// image attachment.
type inlineImages struct {
	protocol graphics.Protocol
	// answered is whether the terminal has answered Init's attributes query; images wait for it.
	answered bool
	// cell is the terminal's cell size in pixels, zero until it reports one.
	cell graphics.Pixels
	// last is the id last sent to the terminal; 0 names no image.
	last uint32
	// shown is by attachment id rather than blob: one blob attached twice sits at two indents, and
	// one image id holds one placement.
	shown map[string]shownImage
}

// toRead marks each of attachments that graphics can decode and that has no state yet as being
// read, and returns them.
func (im inlineImages) toRead(attachments []timeline.Attachment) (inlineImages, []timeline.Attachment) {
	im.shown = maps.Clone(im.shown)
	var reads []timeline.Attachment
	for _, a := range attachments {
		if _, ok := im.shown[a.ID]; ok || !graphics.Decodable(a.MIME) {
			continue
		}
		im.shown[a.ID] = shownImage{state: imageReading}
		reads = append(reads, a)
	}

	return im, reads
}

// read keeps img as the image of the attachment with attachmentID, or marks it failed on err.
func (im inlineImages) read(attachmentID string, img graphics.Image, err error) inlineImages {
	im.shown = maps.Clone(im.shown)
	im.shown[attachmentID] = shownImage{state: imageRead, img: img}
	if err != nil {
		im.shown[attachmentID] = shownImage{state: imageFailed}
	}

	return im
}

// send marks each image of order that is read and not sent as sent, once a terminal that shows
// images has answered, and returns the sequences sending it and placing it in gridFor's grid. A
// sent image keeps only its size.
func (im inlineImages) send(order []timeline.Attachment, gridFor func(timeline.Attachment, graphics.Image) graphics.Grid) (inlineImages, string) {
	if im.protocol != graphics.Kitty || !im.answered {
		return im, ""
	}

	im.shown = maps.Clone(im.shown)
	var sends strings.Builder
	for _, a := range order {
		shown := im.shown[a.ID]
		if shown.state != imageRead {
			continue
		}
		im.last++
		shown.state, shown.id, shown.grid = imageSent, im.last, gridFor(a, shown.img)
		sends.WriteString(graphics.Transmit(shown.id, shown.img.PNG) + graphics.Place(shown.id, shown.grid))
		shown.img = graphics.Image{Width: shown.img.Width, Height: shown.img.Height}
		im.shown[a.ID] = shown
	}

	return im, sends.String()
}

// place returns the sequences placing each sent image of order again whose grid has changed.
func (im inlineImages) place(order []timeline.Attachment, gridFor func(timeline.Attachment, graphics.Image) graphics.Grid) (inlineImages, string) {
	im.shown = maps.Clone(im.shown)
	var places strings.Builder
	for _, a := range order {
		shown := im.shown[a.ID]
		if shown.state != imageSent {
			continue
		}
		if g := gridFor(a, shown.img); g != shown.grid {
			shown.grid = g
			im.shown[a.ID] = shown
			places.WriteString(graphics.Place(shown.id, g))
		}
	}

	return im, places.String()
}
