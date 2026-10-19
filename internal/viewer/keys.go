package viewer

import "charm.land/bubbles/v2/key"

type keyMap struct {
	up, down, pageUp, pageDown, top, bottom key.Binding
	expand, source, tag, contains, jump     key.Binding
	refresh, help, submit, cancel, quit     key.Binding
	interrupt                               key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		pageUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		pageDown:  key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		top:       key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g/home", "first")),
		bottom:    key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G/end", "last")),
		expand:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand")),
		source:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "source")),
		tag:       key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tag")),
		contains:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "text")),
		jump:      key.NewBinding(key.WithKeys("@"), key.WithHelp("@", "jump")),
		refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys")),
		submit:    key.NewBinding(key.WithKeys("enter")),
		cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
		quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		interrupt: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "interrupt")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.expand, k.source, k.tag, k.contains, k.jump, k.help, k.quit}
}

// FullHelp spells out the descriptions ShortHelp keeps to one word so the line fits 80 columns.
func (k keyMap) FullHelp() [][]key.Binding {
	source, tag, contains, jump := k.source, k.tag, k.contains, k.jump
	source.SetHelp(source.Help().Key, "filter by source")
	tag.SetHelp(tag.Help().Key, "filter by tag")
	contains.SetHelp(contains.Help().Key, "filter by text")
	jump.SetHelp(jump.Help().Key, "jump to time")

	up, down := k.up, k.down
	up.SetHelp(up.Help().Key, "up/scroll")
	down.SetHelp(down.Help().Key, "dn/scroll")

	return [][]key.Binding{
		{up, down, k.pageUp, k.pageDown, k.top, k.bottom},
		{k.expand, jump, k.refresh},
		{source, tag, contains},
		{k.help, k.cancel, k.quit, k.interrupt},
	}
}
