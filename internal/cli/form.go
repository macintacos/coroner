package cli

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"
)

// formChoices are what a form's text fields pick from or suggest, by field name.
type formChoices struct {
	// options makes the field a Select instead of an Input.
	options     map[string][]huh.Option[string]
	suggestions map[string][]string
}

// formValues is the submit adapter: the fieldValues a form's fields hold, where an empty value,
// a false confirm and a blank line are not given. fields must come from d.formFields.
func (d definition) formValues(fields []huh.Field) fieldValues {
	values := fieldValues{}
	for _, formField := range fields {
		i := slices.IndexFunc(d.fields, func(f field) bool { return f.name == formField.GetKey() })
		given := d.fields[i].formValue(formField.GetValue())
		if len(given) > 0 {
			values[formField.GetKey()] = given
		}
	}

	return values
}

// formValue returns the values a form field holding value gives f.
func (f field) formValue(value any) []string {
	switch value := value.(type) {
	case bool:
		if value {
			return []string{"true"}
		}
	case string:
		if f.typ == stringsField {
			return valueLines(value)
		}
		if value != "" {
			return []string{value}
		}
	}

	return nil
}

// formFields returns a form field for each of d's fields, keyed by field name, opening with in's
// values, picking from or suggesting choices, showing its usage and running its check.
func (d definition) formFields(env checkEnv, in fieldValues, choices formChoices) []huh.Field {
	bound := make([]*string, len(d.fields))
	for i, f := range d.fields {
		bound[i] = new(strings.Join(in.values(f.name), "\n"))
	}
	fields := make([]huh.Field, len(d.fields))
	form := fieldForm{
		// Read when a check or preview runs, so a zone typed into the form applies to its times.
		env:     func() checkEnv { return typedZone(env, d, fields) },
		choices: choices,
	}
	tz := slices.IndexFunc(d.fields, func(f field) bool { return f.name == "tz" })
	for i, f := range d.fields {
		// A description re-renders when its own value or the zone its times are read in changes.
		bindings := []*string{bound[i]}
		if tz != -1 {
			bindings = append(bindings, bound[tz])
		}
		fields[i] = f.formField(bound[i], bindings, form)
	}

	return fields
}

// fieldForm is what each field of a form reads beyond its own value.
type fieldForm struct {
	env     func() checkEnv
	choices formChoices
}

// typedZone returns env in the zone the tz field of d's form fields holds, when timeZone accepts it.
func typedZone(env checkEnv, d definition, fields []huh.Field) checkEnv {
	i := slices.IndexFunc(d.fields, func(f field) bool { return f.name == "tz" })
	if i == -1 {
		return env
	}
	tz := fmt.Sprint(fields[i].GetValue())
	z, err := timeZone(&tz, "", env.now.Location())
	if err == nil {
		env.zone = z
	}

	return env
}

func (f field) formField(bound *string, bindings []*string, form fieldForm) huh.Field {
	// As pflag.UnquoteUsage prints it: the first `placeholder` loses its backquotes.
	help := strings.Replace(f.usage, "`", "", 2)
	title := f.name
	if f.required {
		title += " (required)"
	}
	// An empty value is a field not given, which the flag side never checks either, except a
	// required field's: the form flags that here so it need not wait for submission.
	check := func(value string) error {
		switch {
		case value == "" && f.required:
			return &Error{Kind: KindUsage, Message: f.name + " is required"}
		case f.check == nil || value == "":
			return nil
		}

		return f.check(value, form.env())
	}

	switch {
	case f.typ == boolField:
		b, _ := strconv.ParseBool(*bound)
		return huh.NewConfirm().Key(f.name).Title(title).Description(help).Value(&b)
	case f.typ == stringsField:
		return huh.NewText().Key(f.name).Title(title).Description(help).Value(bound).Validate(func(text string) error {
			return checkLines(text, check)
		})
	case form.choices.options[f.name] != nil:
		selectField := huh.NewSelect[string]().Key(f.name).Title(title).Description(help).Value(bound).
			Options(withGiven(form.choices.options[f.name], *bound)...).Validate(check)
		return selectField.DescriptionFunc(func() string {
			return f.description(help, fmt.Sprint(selectField.GetValue()), form.env())
		}, bindings)
	}

	input := huh.NewInput().Key(f.name).Title(title).Description(help).Value(bound).
		Suggestions(form.choices.suggestions[f.name]).Validate(check)
	return input.DescriptionFunc(func() string {
		return f.description(help, fmt.Sprint(input.GetValue()), form.env())
	}, bindings)
}

// withGiven returns options led by given when no option holds it, since a Select overwrites a value
// none holds with its first option, and the field's check must judge it instead.
func withGiven(options []huh.Option[string], given string) []huh.Option[string] {
	if given == "" || slices.ContainsFunc(options, func(o huh.Option[string]) bool { return o.Value == given }) {
		return options
	}

	return append([]huh.Option[string]{huh.NewOption(given, given)}, options...)
}

// description returns help, then value's preview or, with none, the hint of the check it fails;
// huh's error line shows only the message.
func (f field) description(help, value string, env checkEnv) string {
	line := ""
	switch {
	case f.preview != nil:
		line = f.preview(value, env)
	case f.check != nil && value != "":
		if failure, ok := errors.AsType[*Error](f.check(value, env)); ok {
			line = failure.Hint
		}
	}
	if line == "" {
		return help
	}

	return help + "\n" + line
}

// checkLines runs check on each value line of text.
func checkLines(text string, check func(string) error) error {
	for _, line := range valueLines(text) {
		err := check(line)
		if err != nil {
			return err
		}
	}

	return nil
}

// valueLines returns the lines of text that hold a value: every one but those holding only white space.
func valueLines(text string) []string {
	return slices.DeleteFunc(strings.Split(text, "\n"), func(line string) bool { return strings.TrimSpace(line) == "" })
}
