// Package viewer is the read-only terminal viewer 'coroner timeline view' opens on a terminal.
package viewer

import (
	"context"
	"errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/macintacos/coroner/internal/store"
	"github.com/macintacos/coroner/internal/timeline"
)

// ErrInterrupted is what Run returns when Ctrl-C, or the end of its context, stops the viewer.
var ErrInterrupted = errors.New("interrupted")

// Snapshot is one read of a timeline.
type Snapshot struct {
	Timeline timeline.Timeline
	// Events are in timeline.CompareEvents order.
	Events []timeline.Event
	// Annotations are every annotation in the timeline, oldest first.
	Annotations []timeline.Annotation
}

// Options is what the viewer shows and how it reads the timeline again.
type Options struct {
	Snapshot Snapshot
	// Filter selected Snapshot's events; the viewer changes only its Sources, Tags and Contains.
	Filter store.EventFilter
	// Load reads the timeline again under a filter. It must open and close the database itself,
	// so the viewer holds nothing open between reads.
	Load func(store.EventFilter) (Snapshot, error)
	// Location is the zone times show in, and typed times are read in.
	Location *time.Location
	Now      func() time.Time
}

// Terminal is where the viewer draws and reads keys.
type Terminal struct {
	In      io.Reader
	Out     io.Writer
	NoColor bool
}

// Run shows the viewer on term until q, which returns nil, or Ctrl-C or the end of ctx, which
// return ErrInterrupted.
func Run(ctx context.Context, term Terminal, opts Options) error {
	programOptions := []tea.ProgramOption{tea.WithContext(ctx), tea.WithInput(term.In), tea.WithOutput(term.Out)}
	if term.NoColor {
		programOptions = append(programOptions, tea.WithColorProfile(colorprofile.ASCII))
	}

	_, err := tea.NewProgram(New(opts), programOptions...).Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		return ErrInterrupted
	}

	return err
}
