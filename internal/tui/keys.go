package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every binding used across screens. Screens advertise the subset
// that applies via shortHelp.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Toggle   key.Binding
	Expand   key.Binding
	Collapse key.Binding
	All      key.Binding
	None     key.Binding
	View     key.Binding
	Filter   key.Binding
	Confirm  key.Binding
	Back     key.Binding
	Quit     key.Binding
	Help     key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Toggle:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		Expand:   key.NewBinding(key.WithKeys("right", "l", "enter"), key.WithHelp("→", "expand")),
		Collapse: key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "collapse")),
		All:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
		None:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "none")),
		View:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tree/list")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Confirm:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "continue")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:     key.NewBinding(key.WithKeys("ctrl+c", "q"), key.WithHelp("q", "quit")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}
