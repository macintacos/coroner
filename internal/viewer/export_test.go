package viewer

import "time"

func PickerFields(row TimelineRow, loc *time.Location) []string {
	return pickerFields(row, loc)
}
