package cli

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// formChoices are what a form's text fields pick from or suggest, and what a list field shows
// under the value picked, by field name.
type formChoices struct {
	// options makes the field a Select instead of an Input.
	options     map[string][]huh.Option[string]
	suggestions map[string][]string
	// completions are what a list field suggests for what is typed, computed again after every key
	// in place of fixed suggestions.
	completions map[string]func(typed string) []string
	// thumbnails return the lines shown under a list field's value, and a command the terminal
	// needs to show them.
	thumbnails map[string]func(value string) (string, tea.Cmd)
	// placeholders are shown in an empty Input and are not its value.
	placeholders map[string]string
}

// formValues is the submit adapter: the fieldValues a form's fields hold, where an empty value,
// a false confirm and a blank line are not given. fields must come from d.formFields.
func (d definition) formValues(fields []huh.Field) fieldValues {
	values := fieldValues{}
	for _, formField := range fields {
		given := d.fields[d.fieldIndex(formField.GetKey())].formValue(formField.GetValue())
		if len(given) > 0 {
			values[formField.GetKey()] = given
		}
	}

	return values
}

// formGroups returns fields, which must come from d.formFields, as the form's pages: one page
// when no field is marked more, else the unmarked fields closed by a More options confirm, then
// the marked fields, skipped while it is No. It opens Yes when in gives a marked field, so no
// such value applies unseen.
func (d definition) formGroups(fields []huh.Field, in fieldValues) []formGroup {
	var shown, behind []huh.Field
	var titles []string
	open := false
	for i, f := range d.fields {
		if !f.more {
			shown = append(shown, fields[i])
			continue
		}
		behind = append(behind, fields[i])
		titles = append(titles, f.formTitle())
		open = open || in.given(f.name)
	}
	if len(behind) == 0 {
		return []formGroup{{fields: fields}}
	}
	confirm := huh.NewConfirm().Key("more-options").Title("🔧 More options?").
		Description(strings.Join(titles, ", ")).Value(&open)
	// Value can rebind the confirm, so read what it holds.
	hide := func() bool {
		yes, _ := confirm.GetValue().(bool)
		return !yes
	}

	return []formGroup{{fields: append(shown, confirm)}, {fields: behind, hide: hide}}
}

// shownFields returns the fields of d the form showed: those of groups not hidden, without
// fields d does not declare, such as the More options confirm, plus the tz field when hidden.
func (d definition) shownFields(groups []formGroup) []huh.Field {
	var shown []huh.Field
	for _, g := range groups {
		hidden := g.hide != nil && g.hide()
		for _, formField := range g.fields {
			key := formField.GetKey()
			// Every time the form shows is read in this zone, so it always applies.
			if hidden && key != "tz" {
				continue
			}
			if d.fieldIndex(key) != -1 {
				shown = append(shown, formField)
			}
		}
	}

	return shown
}

// formValue returns the values a form field holding value gives f.
func (f field) formValue(value any) []string {
	switch value := value.(type) {
	case bool:
		if value {
			return []string{"true"}
		}
	case []string:
		if len(value) > 0 {
			return value
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
		in:      in,
	}
	tz, at := d.fieldIndex("tz"), d.fieldIndex("at")
	previewsAt := tz != -1 && at != -1 && d.fields[at].preview != nil
	for i, f := range d.fields {
		// A description re-renders when its own value or the zone its times are read in changes.
		bindings := []*string{bound[i]}
		if tz != -1 {
			bindings = append(bindings, bound[tz])
		}
		if i == tz && previewsAt {
			f.preview = zonePreview(f.check, d.fields[at].preview, func() string { return fmt.Sprint(fields[at].GetValue()) })
			bindings = append(bindings, bound[at])
		}
		fields[i] = f.formField(bound[i], bindings, form)
	}

	return fields
}

// fieldForm is what each field of a form reads beyond its own value.
type fieldForm struct {
	env     func() checkEnv
	choices formChoices
	// in is the values the form opens with.
	in fieldValues
}

// zonePreview returns the tz field's preview: at's value previewed in the zone typed, or the hint
// of the check that zone fails.
func zonePreview(check func(string, checkEnv) error, preview func(string, checkEnv) string, at func() string) func(string, checkEnv) string {
	return func(value string, env checkEnv) string {
		if check != nil && value != "" {
			if failure, ok := errors.AsType[*Error](check(value, env)); ok {
				return failure.Hint
			}
		}

		return preview(at(), env)
	}
}

// typedZone returns env in the zone the tz field of d's form fields holds, when timeZone accepts it.
func typedZone(env checkEnv, d definition, fields []huh.Field) checkEnv {
	i := d.fieldIndex("tz")
	if i == -1 || fields[i] == nil {
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
	title := f.formHeading()
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
	// Before the repeatable cases: the timeline picker selects one value of -t, a repeatable flag.
	case form.choices.options[f.name] != nil:
		selectField := huh.NewSelect[string]().Key(f.name).Title(title).Description(help).Value(bound).
			Options(withGiven(form.choices.options[f.name], *bound)...).Validate(check)
		return selectField.DescriptionFunc(func() string {
			return f.description(help, fmt.Sprint(selectField.GetValue()), form.env())
		}, bindings)
	case f.typ == stringsField && f.list:
		return newListField(huh.NewInput().Key(f.name).Title(title), help, form.in.values(f.name), check).
			withSuggestions(form.choices.suggestions[f.name]).withCompletions(form.choices.completions[f.name]).
			withThumbnail(form.choices.thumbnails[f.name])
	case f.typ == stringsField:
		return huh.NewText().Key(f.name).Title(title).Description(help).Value(bound).Validate(func(text string) error {
			return checkLines(text, check)
		})
	}

	suggestions := form.choices.suggestions[f.name]
	input := huh.NewInput().Key(f.name).Title(title).Description(help).Value(bound).
		Suggestions(suggestions).Placeholder(form.choices.placeholders[f.name]).Validate(check)
	describe := func() string {
		return f.description(help, fmt.Sprint(input.GetValue()), form.env())
	}
	switch {
	case f.suggest != nil:
		return suggestingInput{Input: input.DescriptionFunc(describe, bindings), bound: bound, suggest: f.suggest}
	case len(suggestions) > 0:
		return newMatchingInput(input, describe, suggestions)
	}

	return input.DescriptionFunc(describe, bindings)
}

// formHeading returns what a form shows over f: its icon, when it has one, then its title.
func (f field) formHeading() string {
	return strings.TrimSpace(fieldIcon(f.name) + " " + f.formTitle())
}

// fieldIcon returns the icon a form shows before the title of the field called name, or "" for
// none. Each has emoji presentation, so terminals draw it two cells wide and the titles line up.
func fieldIcon(name string) string {
	return map[string]string{
		"timeline": "📒", "text": "📝", "body": "📝", "at": "🕒", "ended": "⌛", "approximate": "🤔",
		"tz": "🌐", "source": "📡", "tag": "🔖", "url": "🔗", "author": "👤", "attach": "📎", "id": "🆔",
		"dry-run": "🧪", "event": "📍", "kind": "🔎", "name": "📛", "title": "📄", "ref": "🎫",
	}[name]
}

// suggestingInput is an Input that ctrl+e sets to the valid value suggest offers for what it holds.
type suggestingInput struct {
	*huh.Input
	// bound is the value the Input's description re-renders on.
	bound   *string
	suggest func(value string) string
}

func useSuggestionKey() key.Binding {
	return key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "use suggestion"))
}

func (s suggestingInput) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if suggestion := s.suggest(fmt.Sprint(s.GetValue())); ok && suggestion != "" && key.Matches(press, useSuggestionKey()) {
		*s.bound = suggestion
		s.Value(s.bound)
	}
	// The Input clears the error the rejected value left on any key.
	_, cmd := s.Input.Update(msg)

	return s, cmd
}

func (s suggestingInput) KeyBinds() []key.Binding {
	if s.suggest(fmt.Sprint(s.GetValue())) == "" {
		return s.Input.KeyBinds()
	}

	return append([]key.Binding{useSuggestionKey()}, s.Input.KeyBinds()...)
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
