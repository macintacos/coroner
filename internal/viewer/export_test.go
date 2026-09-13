package viewer

import "time"

// PickerColumns is pickerColumns with its fields exported, for specs to compare.
type PickerColumns struct {
	Marker, Name, Events, Last, Title, Refs string
}

func PickerFields(row TimelineRow, loc *time.Location) PickerColumns {
	c := pickerFields(row, loc)
	return PickerColumns{Marker: c.marker, Name: c.name, Events: c.events, Last: c.last, Title: c.title, Refs: c.refs}
}
