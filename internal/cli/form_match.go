package cli

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// matchRow is the row of suggestions a field shows beside huh's inline one: with nothing typed the
// first listShown, else what textinput matches by its own rule, in a window holding the match its
// ghost completes to.
type matchRow struct {
	suggestions []string
	typed       string
	matches     []string
	// current mirrors textinput's current suggestion: reset when the matches change, else moved by
	// its NextSuggestion and PrevSuggestion keys.
	current int
}

func newMatchRow(suggestions []string, typed string) *matchRow {
	r := &matchRow{suggestions: suggestions}
	r.follow(tea.KeyPressMsg{}, typed)

	return r
}

// follow updates r for press, after the field has handled it, leaving typed, and reports whether
// its line changed.
func (r *matchRow) follow(press tea.KeyPressMsg, typed string) bool {
	before := r.line()
	r.typed = typed
	var matches []string
	if typed != "" {
		for _, s := range r.suggestions {
			if strings.HasPrefix(strings.ToLower(s), strings.ToLower(typed)) {
				matches = append(matches, s)
			}
		}
	}
	keys := textinput.DefaultKeyMap()
	switch {
	case !slices.Equal(matches, r.matches):
		r.current = 0
	case len(matches) == 0:
	case key.Matches(press, keys.NextSuggestion):
		r.current = (r.current + 1) % len(matches)
	case key.Matches(press, keys.PrevSuggestion):
		r.current = (r.current - 1 + len(matches)) % len(matches)
	}
	r.matches = matches

	return r.line() != before
}

// line returns the row, or "" when nothing matches what is typed.
func (r *matchRow) line() string {
	shown := r.suggestions
	if r.typed != "" {
		first := max(0, r.current-listShown+1)
		shown = r.matches[first:]
	}
	shown = shown[:min(listShown, len(shown))]
	if len(shown) == 0 {
		return ""
	}
	labels := make([]string, len(shown))
	for i, value := range shown {
		labels[i] = pickerLabel(value)
	}

	return "↳ " + strings.Join(labels, "  ")
}

func cycleKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "cycle matches")),
		key.NewBinding(key.WithKeys("ctrl+n", "ctrl+p"), key.WithHelp("ctrl+n/ctrl+p", "cycle")),
	}
}

// matchingInput is an Input with suggestions that shows its match row under describe's
// description. It sets the description on every update rather than through DescriptionFunc,
// which huh evaluates asynchronously and caches by its bindings, so a window moved by a key that
// leaves the value unchanged would render stale.
type matchingInput struct {
	*huh.Input
	describe func() string
	row      *matchRow
}

func newMatchingInput(input *huh.Input, describe func() string, suggestions []string) matchingInput {
	m := matchingInput{Input: input, describe: describe, row: newMatchRow(suggestions, fmt.Sprint(input.GetValue()))}
	m.describeRow()

	return m
}

func (m matchingInput) Update(msg tea.Msg) (huh.Model, tea.Cmd) {
	_, cmd := m.Input.Update(msg)
	changed := false
	if press, ok := msg.(tea.KeyPressMsg); ok {
		changed = m.row.follow(press, fmt.Sprint(m.GetValue()))
	}
	m.describeRow()
	// huh fits a form's height to its fields only when a window size arrives.
	if changed {
		cmd = tea.Batch(cmd, tea.RequestWindowSize)
	}

	return m, cmd
}

func (m matchingInput) describeRow() {
	description := m.describe()
	if line := m.row.line(); line != "" {
		description += "\n" + line
	}
	m.Description(description)
}

func (m matchingInput) KeyBinds() []key.Binding {
	if len(m.row.matches) == 0 {
		return m.Input.KeyBinds()
	}

	return append(cycleKeys(), m.Input.KeyBinds()...)
}
