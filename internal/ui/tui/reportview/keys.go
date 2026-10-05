package reportview

import "charm.land/bubbles/v2/key"

// keyMap holds the key bindings of the report view. It implements
// help.KeyMap.
type keyMap struct {
	Up, Down   key.Binding // select a group
	Next, Prev key.Binding // change the metric
	Help       key.Binding
	Quit       key.Binding
}

// newKeyMap returns the bindings. Without groups, only Quit is enabled.
func newKeyMap(hasGroups bool) keyMap {
	k := keyMap{
		Up:   key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous group")),
		Down: key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next group")),
		Next: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next metric")),
		Prev: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous metric")),
		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit: key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
	}
	for _, b := range []*key.Binding{&k.Up, &k.Down, &k.Next, &k.Prev, &k.Help} {
		b.SetEnabled(hasGroups)
	}
	return k
}

// ShortHelp returns the bindings of the hint in the title line.
func (k keyMap) ShortHelp() []key.Binding { return []key.Binding{k.Next, k.Help, k.Quit} }

// FullHelp returns the bindings of the full help, in columns.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down}, {k.Next, k.Prev}, {k.Help, k.Quit}}
}
