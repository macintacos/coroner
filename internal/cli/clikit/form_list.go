package clikit

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// ListField is the form field of a repeatable field that takes its values one at a time: enter adds
// what is typed, up and down pick a value while nothing is typed, else cycle the matches of its
// suggestions, and backspace removes the picked one. Its value is its values, a []string. Changing
// them asks for the window size, since huh fits a form's height to its fields only when one arrives.
type ListField struct {
	*huh.Input
	help   string
	Values []string
	// picked is the index of the value up and down picked, or -1 while typing.
	picked int
	check  func(string) error
	// path, when set, turns what is typed into the value added.
	path func(typed string) string
	err  error
	// thumbnail, when set, returns what shows under the picked value, or the last one added, and
	// the command the terminal needs to show it; send holds that command until the next update.
	thumbnail func(value string) (string, tea.Cmd)
	send      tea.Cmd
	// row and complete, when set, are the match row of its suggestions and what returns them for
	// what is typed.
	row      *matchRow
	complete func(typed string) []string
	// preview, when set, returns the line shown above the match row for what is typed; a held value
	// whose preview marks it new carries that mark.
	preview func(value string) string
}

// listShown is how many values a list field, or a match row, shows at once, so a long list never
// pushes its typing line off the screen.
const listShown = 5

// listKeys are the keys a list field reads before its Input does; up and down only while nothing is
// typed.
type listKeys struct {
	add, up, down, remove key.Binding
}

func newListKeys() listKeys {
	return listKeys{
		add:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "add")),
		up:     key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "pick")),
		down:   key.NewBinding(key.WithKeys("down")),
		remove: key.NewBinding(key.WithKeys("backspace", "delete"), key.WithHelp("backspace", "remove")),
	}
}

// newListField returns a list field typing into input, showing help, opening with values, and
// adding only a value check accepts.
func newListField(input *huh.Input, help string, values []string, check func(string) error) *ListField {
	values = slices.DeleteFunc(slices.Clone(values), func(value string) bool { return strings.TrimSpace(value) == "" })
	l := &ListField{Input: input, help: help, Values: values, picked: -1, check: check}
	l.show()

	return l
}

// withThumbnail returns l showing thumbnail's lines under its value.
func (l *ListField) withThumbnail(thumbnail func(value string) (string, tea.Cmd)) *ListField {
	if thumbnail == nil {
		return l
	}
	l.thumbnail = thumbnail
	l.show()

	return l
}

// withPath returns l adding what path turns typed values into.
func (l *ListField) withPath(path func(typed string) string) *ListField {
	l.path = path

	return l
}

// withPreview returns l previewing values with preview, in the env it reads.
func (l *ListField) withPreview(preview func(string, CheckEnv) string, env func() CheckEnv) *ListField {
	if preview == nil {
		return l
	}
	l.preview = func(value string) string { return preview(value, env()) }
	l.show()

	return l
}

// typedPreview returns the preview of what is typed, or "" with none.
func (l *ListField) typedPreview() string {
	if l.preview == nil || l.Typed() == "" {
		return ""
	}

	return l.preview(l.Typed())
}

// withSuggestions returns l suggesting suggestions, and showing a row of what is typed matches.
func (l *ListField) withSuggestions(suggestions []string) *ListField {
	if len(suggestions) == 0 {
		return l
	}

	return l.withCompletions(func(string) []string { return suggestions })
}

// withCompletions returns l suggesting what complete returns for what is typed, and showing a row of
// what is typed matches.
func (l *ListField) withCompletions(complete func(typed string) []string) *ListField {
	if complete == nil {
		return l
	}
	typed := fmt.Sprint(l.Input.GetValue())
	l.complete = complete
	l.row = newMatchRow(nil, typed)
	l.refreshSuggestions(typed)
	l.row.follow(tea.KeyPressMsg{}, typed)
	l.show()

	return l
}

// refreshSuggestions sets the suggestions complete returns for typed.
func (l *ListField) refreshSuggestions(typed string) {
	l.row.suggestions = l.complete(typed)
	if len(l.row.suggestions) == 0 {
		// textinput keeps its matches when suggestions turn off and ghosts them; "" matches nothing.
		l.Suggestions([]string{""})
	}
	l.Suggestions(l.row.suggestions)
}

// Update refreshes the suggestions after a key or a paste, which textinput inserts without
// matching it.
func (l *ListField) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	shownPreview := l.typedPreview()
	model, cmd := l.update(msg)
	press, isPress := msg.(tea.KeyPressMsg)
	_, pasted := msg.(tea.PasteMsg)
	changed := l.typedPreview() != shownPreview
	if (isPress || pasted) && l.row != nil {
		typed, shownRow := fmt.Sprint(l.Input.GetValue()), l.row.line()
		if typed != l.row.typed {
			l.refreshSuggestions(typed)
		}
		l.row.follow(press, typed)
		changed = changed || l.row.line() != shownRow
	}
	if changed {
		l.show()
		cmd = tea.Batch(cmd, tea.RequestWindowSize)
	}
	return model, tea.Batch(cmd, l.takeSend())
}

// takeSend returns the command the last show left, and forgets it.
func (l *ListField) takeSend() tea.Cmd {
	send := l.send
	l.send = nil

	return send
}

func (l *ListField) update(msg tea.Msg) (huh.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		_, cmd := l.Input.Update(msg)
		return l, cmd
	}

	keys := newListKeys()
	switch {
	case key.Matches(press, keys.add) && l.Typed() != "":
		return l, l.addTyped()
	case key.Matches(press, keys.up) && l.Typed() == "" && len(l.Values) > 0:
		l.pick(true)
		return l, nil
	case key.Matches(press, keys.down) && l.Typed() == "" && len(l.Values) > 0:
		l.pick(false)
		return l, nil
	case key.Matches(press, keys.remove) && l.picked != -1:
		l.Values = slices.Delete(l.Values, l.picked, l.picked+1)
		l.picked = -1
		l.show()
		return l, tea.RequestWindowSize
	}

	l.err = nil
	if l.picked != -1 {
		l.picked = -1
		l.show()
	}
	_, cmd := l.Input.Update(msg)

	return l, cmd
}

// Blur adds a value left typed, so leaving the field never drops it.
func (l *ListField) Blur() tea.Cmd {
	var added tea.Cmd
	if l.Typed() != "" {
		added = l.addTyped()
	}

	return tea.Batch(added, l.Input.Blur(), l.takeSend())
}

func (l *ListField) Error() error {
	return l.err
}

// GetValue returns a copy of the values the field holds, as huh.Field requires.
func (l *ListField) GetValue() any {
	return slices.Clone(l.Values)
}

// KeyBinds returns only the keys that act on what the field holds: enter adds, in place of the
// Input's enter, while something is typed.
func (l *ListField) KeyBinds() []key.Binding {
	keys := newListKeys()
	var binds []key.Binding
	input := l.Input.KeyBinds()
	if l.Typed() != "" {
		binds = append(binds, keys.add)
		input = slices.DeleteFunc(input, func(b key.Binding) bool { return slices.Contains(b.Keys(), "enter") })
	}
	if l.Typed() != "" && l.row != nil && len(l.row.matches) > 0 {
		binds = append(binds, cycleKeys()...)
	}
	if l.Typed() == "" && len(l.Values) > 0 {
		binds = append(binds, keys.up)
	}
	if l.picked != -1 {
		binds = append(binds, keys.remove)
	}

	return append(binds, input...)
}

// Typed returns what is typed, or "" when that is only white space.
func (l *ListField) Typed() string {
	typed := fmt.Sprint(l.Input.GetValue())
	if strings.TrimSpace(typed) == "" {
		return ""
	}

	return typed
}

// addTyped adds what is typed, when check accepts it, and clears it.
func (l *ListField) addTyped() tea.Cmd {
	typed := l.Typed()
	if l.path != nil {
		typed = l.path(typed)
	}
	l.err = l.check(typed)
	if l.err != nil {
		return nil
	}

	l.Values = AppendUnique(l.Values, typed)
	l.Value(new(""))
	if l.row != nil {
		// SetValue leaves textinput's matches for what was typed; suggesting again recomputes them.
		l.refreshSuggestions("")
		l.row.follow(tea.KeyPressMsg{}, "")
	}
	l.show()

	return tea.RequestWindowSize
}

// pick moves the pick one value up, from typing to the last value, or down, from the last value back
// to typing.
func (l *ListField) pick(up bool) {
	switch {
	case up && l.picked == -1:
		l.picked = len(l.Values) - 1
	case up:
		l.picked = max(0, l.picked-1)
	case l.picked == len(l.Values)-1:
		l.picked = -1
	case l.picked != -1:
		l.picked++
	}
	l.show()
}

// show puts under the help at most listShown values, the last ones or a window holding the picked
// one, after how many more there are, then the thumbnail of the picked value or the last one. Each
// value is marked ✓, so it never reads as a suggestion, and the picked one ×, which backspace
// removes (> would read as the Input's prompt).
func (l *ListField) show() {
	first := max(0, len(l.Values)-listShown)
	if l.picked != -1 {
		first = min(first, l.picked)
	}
	shown := l.Values[first:min(first+listShown, len(l.Values))]

	var b strings.Builder
	b.WriteString(l.help)
	if hidden := len(l.Values) - len(shown); hidden > 0 {
		fmt.Fprintf(&b, "\n  … %d more", hidden)
	}
	for i, value := range shown {
		marker := "✓ "
		if first+i == l.picked {
			marker = "× "
		}
		b.WriteString("\n" + marker + PickerLabel(value) + l.heldMark(value))
	}
	if l.thumbnail != nil && len(l.Values) > 0 {
		value := l.Values[len(l.Values)-1]
		if l.picked != -1 {
			value = l.Values[l.picked]
		}
		var lines string
		lines, l.send = l.thumbnail(value)
		if lines != "" {
			b.WriteString("\n" + lines)
		}
	}
	if preview := l.typedPreview(); preview != "" {
		b.WriteString("\n" + preview)
	}
	if l.row != nil && l.row.line() != "" {
		b.WriteString("\n" + l.row.line())
	}

	l.Description(b.String())
}

// heldMark returns what follows a held value: its preview when that marks it new, else "".
func (l *ListField) heldMark(value string) string {
	if l.preview == nil {
		return ""
	}
	if preview := l.preview(value); strings.HasPrefix(preview, newMark) {
		return "  " + preview
	}

	return ""
}
