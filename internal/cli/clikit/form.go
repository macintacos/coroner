package clikit

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

// FormChoices are what a form's text fields pick from or suggest, and what a list field shows
// under the value picked, by field name.
type FormChoices struct {
	// Options makes the field a Select instead of an Input.
	Options     map[string][]huh.Option[string]
	Suggestions map[string][]string
	// Completions are what a list field suggests for what is typed, computed again after every key
	// in place of fixed suggestions.
	Completions map[string]func(typed string) []string
	// Thumbnails return the lines shown under a list field's value, and a command the terminal
	// needs to show them.
	Thumbnails map[string]func(value string) (string, tea.Cmd)
	// Paths turn what is typed into a list field into the value it adds.
	Paths map[string]func(typed string) string
	// placeholders are shown in an empty Input and are not its value.
	placeholders map[string]string
}

// FormValues is the submit adapter: the FieldValues a form's fields hold, where an empty value,
// a false confirm and a blank line are not given. fields must come from d.FormFields.
func (d Definition) FormValues(fields []huh.Field) FieldValues {
	values := FieldValues{}
	for _, formField := range fields {
		given := d.Fields[d.FieldIndex(formField.GetKey())].formValue(formField.GetValue())
		if len(given) > 0 {
			values[formField.GetKey()] = given
		}
	}

	return values
}

// FormGroups returns fields, which must come from d.FormFields, as the form's pages: one page
// when no field is marked more, else the unmarked fields closed by a More options confirm, then
// the marked fields, skipped while it is No. It opens Yes when in gives a marked field, so no
// such value applies unseen.
func (d Definition) FormGroups(fields []huh.Field, in FieldValues) []FormGroup {
	var shown, behind []huh.Field
	var titles []string
	open := false
	for i, f := range d.Fields {
		if !f.More {
			shown = append(shown, fields[i])
			continue
		}
		behind = append(behind, fields[i])
		titles = append(titles, f.FormTitle())
		open = open || in.Given(f.Name)
	}
	if len(behind) == 0 {
		return []FormGroup{{Fields: fields}}
	}
	confirm := huh.NewConfirm().Key("more-options").Title("🔧 More options?").
		Description(strings.Join(titles, ", ")).Value(&open)
	// Value can rebind the confirm, so read what it holds.
	hide := func() bool {
		yes, _ := confirm.GetValue().(bool)
		return !yes
	}

	return []FormGroup{{Fields: append(shown, confirm)}, {Fields: behind, Hide: hide}}
}

// shownFields returns the fields of d the form showed: those of groups not hidden, without
// fields d does not declare, such as the More options confirm, plus the tz field when hidden.
func (d Definition) shownFields(groups []FormGroup) []huh.Field {
	var shown []huh.Field
	for _, g := range groups {
		hidden := g.Hide != nil && g.Hide()
		for _, formField := range g.Fields {
			key := formField.GetKey()
			// Every time the form shows is read in this zone, so it always applies.
			if hidden && key != "tz" {
				continue
			}
			if d.FieldIndex(key) != -1 {
				shown = append(shown, formField)
			}
		}
	}

	return shown
}

// formValue returns the values a form field holding value gives f.
func (f Field) formValue(value any) []string {
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
		if f.Typ == StringsField {
			return valueLines(value)
		}
		if value != "" {
			return []string{value}
		}
	}

	return nil
}

// FormFields returns a form field for each of d's fields, keyed by field name, opening with in's
// values, picking from or suggesting choices, showing its usage and running its check.
func (d Definition) FormFields(env CheckEnv, in FieldValues, choices FormChoices) []huh.Field {
	bound := make([]*string, len(d.Fields))
	for i, f := range d.Fields {
		bound[i] = new(strings.Join(in.Values(f.Name), "\n"))
	}
	fields := make([]huh.Field, len(d.Fields))
	form := fieldForm{
		// Read when a check or preview runs, so a zone typed into the form applies to its times.
		env:     func() CheckEnv { return typedZone(env, d, fields) },
		choices: choices,
		in:      in,
	}
	tz, at := d.FieldIndex("tz"), d.FieldIndex("at")
	previewsAt := tz != -1 && at != -1 && d.Fields[at].Preview != nil
	for i, f := range d.Fields {
		// A description re-renders when its own value or the zone its times are read in changes.
		bindings := []*string{bound[i]}
		if tz != -1 {
			bindings = append(bindings, bound[tz])
		}
		if i == tz && previewsAt {
			f.Preview = zonePreview(f.Check, d.Fields[at].Preview, func() string { return fmt.Sprint(fields[at].GetValue()) })
			bindings = append(bindings, bound[at])
		}
		fields[i] = f.formField(bound[i], bindings, form)
	}

	return fields
}

// fieldForm is what each field of a form reads beyond its own value.
type fieldForm struct {
	env     func() CheckEnv
	choices FormChoices
	// in is the values the form opens with.
	in FieldValues
}

// zonePreview returns the tz field's preview: at's value previewed in the zone typed, or the hint
// of the check that zone fails.
func zonePreview(check func(string, CheckEnv) error, preview func(string, CheckEnv) string, at func() string) func(string, CheckEnv) string {
	return func(value string, env CheckEnv) string {
		if check != nil && value != "" {
			if failure, ok := errors.AsType[*Error](check(value, env)); ok {
				return failure.Hint
			}
		}

		return preview(at(), env)
	}
}

// typedZone returns env in the zone the tz field of d's form fields holds, when TimeZone accepts it.
func typedZone(env CheckEnv, d Definition, fields []huh.Field) CheckEnv {
	i := d.FieldIndex("tz")
	if i == -1 || fields[i] == nil {
		return env
	}
	tz := fmt.Sprint(fields[i].GetValue())
	z, err := TimeZone(&tz, "", env.Now.Location())
	if err == nil {
		env.Zone = z
	}

	return env
}

func (f Field) formField(bound *string, bindings []*string, form fieldForm) huh.Field {
	// As pflag.UnquoteUsage prints it: the first `placeholder` loses its backquotes.
	help := strings.Replace(f.Usage, "`", "", 2)
	title := f.formHeading()
	if f.Required {
		title += " (required)"
	}
	// An empty value is a field not given, which the flag side never checks either, except a
	// required field's: the form flags that here so it need not wait for submission.
	check := func(value string) error {
		switch {
		case value == "" && f.Required:
			return &Error{Kind: KindUsage, Message: f.Name + " is required"}
		case f.Check == nil || value == "":
			return nil
		}

		return f.Check(value, form.env())
	}

	switch {
	case f.Typ == BoolField:
		b, _ := strconv.ParseBool(*bound)
		return huh.NewConfirm().Key(f.Name).Title(title).Description(help).Value(&b)
	// Before the repeatable cases: the timeline picker selects one value of -t, a repeatable flag.
	case form.choices.Options[f.Name] != nil:
		selectField := huh.NewSelect[string]().Key(f.Name).Title(title).Description(help).Value(bound).
			Options(withGiven(form.choices.Options[f.Name], *bound)...).Validate(check)
		return selectField.DescriptionFunc(func() string {
			return f.description(help, fmt.Sprint(selectField.GetValue()), form.env())
		}, bindings)
	case f.Typ == StringsField && f.List:
		return newListField(huh.NewInput().Key(f.Name).Title(title), help, form.in.Values(f.Name), check).
			withSuggestions(form.choices.Suggestions[f.Name]).withCompletions(form.choices.Completions[f.Name]).
			withThumbnail(form.choices.Thumbnails[f.Name]).withPath(form.choices.Paths[f.Name]).withPreview(f.Preview, form.env)
	case f.Typ == StringsField:
		return huh.NewText().Key(f.Name).Title(title).Description(help).Value(bound).Validate(func(text string) error {
			return checkLines(text, check)
		})
	}

	suggestions := form.choices.Suggestions[f.Name]
	input := huh.NewInput().Key(f.Name).Title(title).Description(help).Value(bound).
		Suggestions(suggestions).Placeholder(form.choices.placeholders[f.Name]).Validate(check)
	describe := func() string {
		return f.description(help, fmt.Sprint(input.GetValue()), form.env())
	}
	switch {
	case f.Suggest != nil:
		return suggestingInput{Input: input.DescriptionFunc(describe, bindings), bound: bound, suggest: f.Suggest}
	case len(suggestions) > 0:
		return newMatchingInput(input, describe, suggestions)
	}

	return input.DescriptionFunc(describe, bindings)
}

// formHeading returns what a form shows over f: its icon, when it has one, then its title.
func (f Field) formHeading() string {
	return strings.TrimSpace(FieldIcon(f.Name) + " " + f.FormTitle())
}

// FieldIcon returns the icon a form shows before the title of the field called name, or "" for
// none. Each has emoji presentation, so terminals draw it two cells wide and the titles line up.
func FieldIcon(name string) string {
	return map[string]string{
		"timeline": "📒", "text": "📝", "body": "📝", "at": "🕒", "ended": "⌛", "approximate": "🤔",
		"tz": "🌐", "source": "📡", "tag": "🔖", "url": "🔗", "author": "👤", "attach": "📎",
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
func (f Field) description(help, value string, env CheckEnv) string {
	line := ""
	switch {
	case f.Preview != nil:
		line = f.Preview(value, env)
	case f.Check != nil && value != "":
		if failure, ok := errors.AsType[*Error](f.Check(value, env)); ok {
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
