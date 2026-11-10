package clikit

// ClearKeyword is the value that clears an optional field (A-9); keep in sync with
// timeline.ValidateSourceName and the source.name CHECK, which reject it as a source.
const ClearKeyword = "none"

// FieldEdit is an optional field's new value: set when its field was given, and a nil value
// when it said none.
type FieldEdit struct {
	set   bool
	value *string
}

// Apply returns the field's value after the edit: current unless the edit sets it.
func (f FieldEdit) Apply(current *string) *string {
	if f.set {
		return f.value
	}

	return current
}

// ReadFieldEdit reads the optional field name sets, or clears with none.
func ReadFieldEdit(in FieldValues, name string) FieldEdit {
	value := in.Value(name)
	switch {
	case value == nil:
		return FieldEdit{}
	case *value == ClearKeyword:
		return FieldEdit{set: true}
	}

	return FieldEdit{set: true, value: value}
}
