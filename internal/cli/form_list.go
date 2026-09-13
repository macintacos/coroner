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
// what is typed, up and down pick a value, and backspace removes the picked one. Its value is its
// values as lines, as a Text field's is.
type listField struct {
	*huh.Input
	help   string
	values []string
	// picked is the index of the value up and down picked, or -1 while typing.
	picked int
	check  func(string) error
	err    error
}

// listKeys are the keys a list field reads before its Input does.
type listKeys struct {
	add, pick, remove key.Binding
}

func newListKeys() listKeys {
	return listKeys{
		add:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "add")),
		pick:   key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "pick")),
		remove: key.NewBinding(key.WithKeys("backspace", "delete"), key.WithHelp("backspace", "remove")),
	}
}

// newListField returns a list field typing into input, showing help, opening with values, and
// adding only a value check accepts.
func newListField(input *huh.Input, help string, values []string, check func(string) error) *listField {
	l := &listField{Input: input, help: help, values: values, picked: -1, check: check}
	l.show()

	return l
}

func (l *listField) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		_, cmd := l.Input.Update(msg)
		return l, cmd
	}

	keys := newListKeys()
	switch {
	case key.Matches(press, keys.add) && l.typed() != "":
		l.addTyped()
		return l, nil
	case key.Matches(press, keys.pick) && len(l.values) > 0:
		l.pick(press.Code == tea.KeyUp)
		return l, nil
	case key.Matches(press, keys.remove) && l.picked != -1:
		l.values = slices.Delete(l.values, l.picked, l.picked+1)
		l.picked = -1
		l.show()
		return l, nil
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
	if l.typed() != "" {
		l.addTyped()
	}

	return l.Input.Blur()
}

func (l *listField) Error() error {
	return l.err
}

func (l *listField) GetValue() any {
	return strings.Join(l.values, "\n")
}

func (l *listField) KeyBinds() []key.Binding {
	keys := newListKeys()
	return append([]key.Binding{keys.add, keys.pick, keys.remove}, l.Input.KeyBinds()...)
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
func (l *listField) addTyped() {
	typed := l.typed()
	l.err = l.check(typed)
	if l.err != nil {
		return
	}

	l.values = appendUnique(l.values, typed)
	l.Value(new(""))
	l.show()
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

// show puts the values under the help, the picked one marked as the one backspace removes; > is
// the Input's prompt.
func (l *listField) show() {
	var b strings.Builder
	b.WriteString(l.help)
	for i, value := range l.values {
		marker := "  "
		if i == l.picked {
			marker = "× "
		}
		b.WriteString("\n" + marker + pickerLabel(value))
	}

	l.Description(b.String())
}
