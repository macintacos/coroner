package cli

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// listField is the form field of a repeatable field that takes its values one at a time: enter adds
// what is typed, up and down pick a value while nothing is typed, else cycle the matches of its
// suggestions, and backspace removes the picked one. Its value is its values, a []string. Changing
// them asks for the window size, since huh fits a form's height to its fields only when one arrives.
type listField struct {
	*huh.Input
	help   string
	values []string
	// picked is the index of the value up and down picked, or -1 while typing.
	picked int
	check  func(string) error
	err    error
	// thumbnail, when set, returns what shows under the picked value, or the last one added, and
	// the command the terminal needs to show it; send holds that command until the next update.
	thumbnail func(value string) (string, tea.Cmd)
	send      tea.Cmd
	// row, when set, is the match row of its suggestions.
	row *matchRow
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
func newListField(input *huh.Input, help string, values []string, check func(string) error) *listField {
	values = slices.DeleteFunc(slices.Clone(values), func(value string) bool { return strings.TrimSpace(value) == "" })
	l := &listField{Input: input, help: help, values: values, picked: -1, check: check}
	l.show()

	return l
}

// withThumbnail returns l showing thumbnail's lines under its value.
func (l *listField) withThumbnail(thumbnail func(value string) (string, tea.Cmd)) *listField {
	if thumbnail == nil {
		return l
	}
	l.thumbnail = thumbnail
	l.show()

	return l
}

// withSuggestions returns l suggesting suggestions, and showing a row of what is typed matches.
func (l *listField) withSuggestions(suggestions []string) *listField {
	if len(suggestions) == 0 {
		return l
	}
	l.Suggestions(suggestions)
	l.row = newMatchRow(suggestions, fmt.Sprint(l.Input.GetValue()))
	l.show()

	return l
}

func (l *listField) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	model, cmd := l.update(msg)
	if press, ok := msg.(tea.KeyPressMsg); ok && l.row != nil && l.row.follow(press, fmt.Sprint(l.Input.GetValue())) {
		l.show()
		cmd = tea.Batch(cmd, tea.RequestWindowSize)
	}
	return model, tea.Batch(cmd, l.takeSend())
}

// takeSend returns the command the last show left, and forgets it.
func (l *listField) takeSend() tea.Cmd {
	send := l.send
	l.send = nil

	return send
}

func (l *listField) update(msg tea.Msg) (huh.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		_, cmd := l.Input.Update(msg)
		return l, cmd
	}

	keys := newListKeys()
	switch {
	case key.Matches(press, keys.add) && l.typed() != "":
		return l, l.addTyped()
	case key.Matches(press, keys.up) && l.typed() == "" && len(l.values) > 0:
		l.pick(true)
		return l, nil
	case key.Matches(press, keys.down) && l.typed() == "" && len(l.values) > 0:
		l.pick(false)
		return l, nil
	case key.Matches(press, keys.remove) && l.picked != -1:
		l.values = slices.Delete(l.values, l.picked, l.picked+1)
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
func (l *listField) Blur() tea.Cmd {
	var added tea.Cmd
	if l.typed() != "" {
		added = l.addTyped()
	}

	return tea.Batch(added, l.Input.Blur(), l.takeSend())
}

func (l *listField) Error() error {
	return l.err
}

func (l *listField) GetValue() any {
	return slices.Clone(l.values)
}

// KeyBinds returns only the keys that act on what the field holds: enter adds, in place of the
// Input's enter, while something is typed.
func (l *listField) KeyBinds() []key.Binding {
	keys := newListKeys()
	var binds []key.Binding
	input := l.Input.KeyBinds()
	if l.typed() != "" {
		binds = append(binds, keys.add)
		input = slices.DeleteFunc(input, func(b key.Binding) bool { return slices.Contains(b.Keys(), "enter") })
	}
	if l.typed() != "" && l.row != nil && len(l.row.matches) > 0 {
		binds = append(binds, cycleKeys()...)
	}
	if l.typed() == "" && len(l.values) > 0 {
		binds = append(binds, keys.up)
	}
	if l.picked != -1 {
		binds = append(binds, keys.remove)
	}

	return append(binds, input...)
}

// typed returns what is typed, or "" when that is only white space.
func (l *listField) typed() string {
	typed := fmt.Sprint(l.Input.GetValue())
	if strings.TrimSpace(typed) == "" {
		return ""
	}

	return typed
}

// addTyped adds what is typed, when check accepts it, and clears it.
func (l *listField) addTyped() tea.Cmd {
	typed := l.typed()
	l.err = l.check(typed)
	if l.err != nil {
		return nil
	}

	l.values = appendUnique(l.values, typed)
	l.Value(new(""))
	if l.row != nil {
		// SetValue leaves textinput's matches for what was typed; SetSuggestions recomputes them.
		l.Suggestions(l.row.suggestions)
		l.row.follow(tea.KeyPressMsg{}, "")
	}
	l.show()

	return tea.RequestWindowSize
}

// pick moves the pick one value up, from typing to the last value, or down, from the last value back
// to typing.
func (l *listField) pick(up bool) {
	switch {
	case up && l.picked == -1:
		l.picked = len(l.values) - 1
	case up:
		l.picked = max(0, l.picked-1)
	case l.picked == len(l.values)-1:
		l.picked = -1
	case l.picked != -1:
		l.picked++
	}
	l.show()
}

// show puts under the help at most listShown values, the last ones or a window holding the picked
// one, after how many more there are, then the thumbnail of the picked value or the last one. The
// picked one is marked ×, which backspace removes (> would read as the Input's prompt).
func (l *listField) show() {
	first := max(0, len(l.values)-listShown)
	if l.picked != -1 {
		first = min(first, l.picked)
	}
	shown := l.values[first:min(first+listShown, len(l.values))]

	var b strings.Builder
	b.WriteString(l.help)
	if hidden := len(l.values) - len(shown); hidden > 0 {
		fmt.Fprintf(&b, "\n  … %d more", hidden)
	}
	for i, value := range shown {
		marker := "  "
		if first+i == l.picked {
			marker = "× "
		}
		b.WriteString("\n" + marker + pickerLabel(value))
	}
	if l.thumbnail != nil && len(l.values) > 0 {
		value := l.values[len(l.values)-1]
		if l.picked != -1 {
			value = l.values[l.picked]
		}
		var lines string
		lines, l.send = l.thumbnail(value)
		if lines != "" {
			b.WriteString("\n" + lines)
		}
	}
	if l.row != nil && l.row.line() != "" {
		b.WriteString("\n" + l.row.line())
	}

	l.Description(b.String())
}
