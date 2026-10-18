package viewer

import (
	"time"

	"github.com/charmbracelet/colorprofile"
)

// PickerColumns is pickerColumns with its fields exported, for specs to compare.
type PickerColumns struct {
	Marker, Name, Events, Last, Title, Refs string
}

func PickerFields(row TimelineRow, loc *time.Location) PickerColumns {
	c := pickerFields(row, loc)
	return PickerColumns{Marker: c.marker, Name: c.name, Events: c.events, Last: c.last, Title: c.title, Refs: c.refs}
}

// OnTerminal is onTerminal, for specs to build the viewer Run shows on term.
func OnTerminal(term Terminal, m Model) Model {
	return onTerminal(term, m)
}

// ColorProfile is colorProfile, for specs to check the profile Run pins.
func ColorProfile(term Terminal) colorprofile.Profile {
	return colorProfile(term)
}
