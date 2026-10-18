// Package viewer is the full-screen viewer 'coroner timeline view' opens and the picker
// 'coroner timeline list' opens on a terminal.
package viewer

import (
	"context"
	"errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/macintacos/coroner/internal/graphics"
	"github.com/macintacos/coroner/internal/timeline"
)

// ErrInterrupted is what Run and Pick return when Ctrl-C stops them.
var ErrInterrupted = errors.New("interrupted")

// Snapshot is one read of a timeline, or of several read together as an overlay.
type Snapshot struct {
	// Timelines are in the order they were selected, which picks each one's color.
	Timelines []timeline.Timeline
	// Events are in timeline.CompareEvents order.
	Events []timeline.Event
	// Annotations are every annotation in the timelines, each timeline's oldest first.
	Annotations []timeline.Annotation
	// Attachments are every attachment on the timelines' events and annotations.
	Attachments []timeline.Attachment
}

// Match picks events by source (any of), tag (all of) and text, as event list's flags do.
type Match struct {
	Sources, Tags []string
	Contains      string
}

// Options is what the viewer shows and how it reads the timeline again.
type Options struct {
	Snapshot Snapshot
	// Match is the match Snapshot's events were read under.
	Match Match
	// Load reads the timeline again under a match. It validates the match, opens and closes
	// the database itself, and returns errors whose text is ready to show.
	Load func(Match) (Snapshot, error)
	// Image reads an image attachment and returns its image: decoded for a terminal that shows
	// images, or only its size in pixels otherwise. The viewer calls it in a command, never while
	// rendering, and shows an attachment it cannot read as its placeholder.
	Image func(timeline.Attachment) (graphics.Image, error)
	// Location is the zone times show in, and typed times are read in.
	Location  *time.Location
	ZoneLabel string // shown verbatim in the header, e.g. "America/New_York" or "local"
	// Now is the clock relative times typed at @ are read against.
	Now func() time.Time
}

// Terminal is where the viewer draws and reads keys.
type Terminal struct {
	// In must be the terminal: the viewer reads raw keys from it.
	In  io.Reader
	Out io.Writer
	// NoColor drops colors but keeps bold, faint and reverse, so the cursor still shows.
	NoColor bool
	// Graphics is how the terminal shows images. NoColor shows none: Kitty graphics name an image
	// by the color of the cells it shows through.
	Graphics graphics.Protocol
}

// Run shows the viewer on term until q, which returns nil, Ctrl-C, which returns ErrInterrupted,
// or the end of ctx.
func Run(ctx context.Context, term Terminal, opts Options) error {
	_, err := program(ctx, term, onTerminal(term, New(opts)))
	return err
}

// images is how the viewer shows images on t.
func (t Terminal) images() graphics.Protocol {
	if t.NoColor {
		return graphics.None
	}

	return t.Graphics
}

// onTerminal is m showing images the way term can.
func onTerminal(term Terminal, m Model) Model {
	m.images.protocol = term.images()
	return m
}

// program runs model on term until it quits, returning its final state, or ErrInterrupted on
// Ctrl-C.
func program(ctx context.Context, term Terminal, model tea.Model) (tea.Model, error) {
	// The caller's context already ends on a signal, and the profile is pinned so bubbletea never
	// reads the process environment to pick one.
	final, err := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(term.In), tea.WithOutput(term.Out),
		tea.WithColorProfile(colorProfile(term)), tea.WithoutSignalHandler()).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return final, ErrInterrupted
	}

	return final, err
}

// colorProfile is the color profile the viewer draws in on term.
func colorProfile(term Terminal) colorprofile.Profile {
	switch {
	case term.NoColor:
		return colorprofile.ASCII
	case term.images() == graphics.Kitty:
		// An image's id travels as a 24-bit color, which ANSI would round to one of 16.
		return colorprofile.TrueColor
	}

	return colorprofile.ANSI
}
