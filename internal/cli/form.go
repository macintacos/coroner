package cli

import (
	"strings"

	"charm.land/huh/v2"
)

// formFields returns a form field for each of d's fields, keyed by field name, showing its usage
// and running its check against env.
func (d definition) formFields(env checkEnv) []huh.Field {
	fields := make([]huh.Field, 0, len(d.fields))
	for _, f := range d.fields {
		fields = append(fields, f.formField(env))
	}

	return fields
}

func (f field) formField(env checkEnv) huh.Field {
	// As pflag.UnquoteUsage prints it: the first `placeholder` loses its backquotes.
	help := strings.Replace(f.usage, "`", "", 2)
	// An empty value is a field not given, which the flag side never checks either.
	check := func(value string) error {
		if f.check == nil || value == "" {
			return nil
		}

		return f.check(value, env)
	}

	switch f.typ {
	case boolField:
		return huh.NewConfirm().Key(f.name).Title(f.name).Description(help).Value(new(false))
	case stringsField:
		return huh.NewText().Key(f.name).Title(f.name).Description(help).Value(new("")).Validate(func(text string) error {
			return checkLines(text, check)
		})
	case stringField:
	}

	return huh.NewInput().Key(f.name).Title(f.name).Description(help).Value(new("")).Validate(check)
}

// checkLines runs check on each line of text, where an empty text holds no values.
func checkLines(text string, check func(string) error) error {
	if text == "" {
		return nil
	}

	for line := range strings.Lines(text) {
		err := check(strings.TrimSuffix(line, "\n"))
		if err != nil {
			return err
		}
	}

	return nil
}
