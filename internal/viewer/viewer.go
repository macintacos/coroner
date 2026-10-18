// Package viewer is the read-only full-screen viewer 'coroner timeline view' opens on a terminal.
package viewer

import (
	"context"
	"errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/macintacos/coroner/internal/timeline"
)

// ErrInterrupted is what Run returns when Ctrl-C stops the viewer.
var ErrInterrupted = errors.New("interrupted")

// Snapshot is one read of a timeline.
type Snapshot struct {
	Timeline timeline.Timeline
	// Events are in timeline.CompareEvents order.
	Events []timeline.Event
	// Annotations are every annotation in the timeline, oldest first.
	Annotations []timeline.Annotation
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
}

// Run shows the viewer on term until q, which returns nil, Ctrl-C, which returns ErrInterrupted,
// or the end of ctx.
func Run(ctx context.Context, term Terminal, opts Options) error {
	profile := colorprofile.ANSI
	if term.NoColor {
		profile = colorprofile.ASCII
	}

	// The caller's context already ends on a signal, and the profile is pinned so bubbletea never
	// reads the process environment to pick one.
	_, err := tea.NewProgram(New(opts), tea.WithContext(ctx), tea.WithInput(term.In), tea.WithOutput(term.Out),
		tea.WithColorProfile(profile), tea.WithoutSignalHandler()).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return ErrInterrupted
	}

	return err
}
