package export

import (
	"bytes"
	"encoding/json/v2"
	"reflect"
	"strings"
)

// DifferingKeys returns the JSON keys of a record type whose values encode differently in a and
// b, in declaration order.
func DifferingKeys[T Record](a, b T) []string {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	var keys []string
	for field := range reflect.TypeFor[T]().Fields() {
		ja, errA := json.Marshal(va.FieldByIndex(field.Index).Interface())
		jb, errB := json.Marshal(vb.FieldByIndex(field.Index).Interface())
		if errA != nil || errB != nil || !bytes.Equal(ja, jb) {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			keys = append(keys, name)
		}
	}

	return keys
}
