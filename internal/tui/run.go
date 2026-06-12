package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run launches the interactive TUI and blocks until the user exits. The
// discovered source (and its temp extraction dir) is released on exit.
func Run(opt Options) error {
	m := New(opt)

	// Detect the terminal background colour before tea.NewProgram starts its
	// input-reader goroutine.  glamour.WithAutoStyle() issues the same OSC 11
	// terminal query; if that query runs inside Update() the Bubble Tea reader
	// goroutine races for the terminal response, the query times out (up to 5 s),
	// and the spinner freezes.  By resolving it here we can use
	// glamour.WithStandardStyle("dark"/"light") everywhere else — a pure
	// in-memory style lookup with no terminal I/O.
	if lipgloss.HasDarkBackground() {
		m.glamStyle = "dark"
	} else {
		m.glamStyle = "light"
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if opt.Ctx != nil {
		opts = append(opts, tea.WithContext(opt.Ctx))
	}
	p := tea.NewProgram(m, opts...)
	final, err := p.Run()
	if app, ok := final.(*App); ok && app.discovered != nil {
		_ = app.discovered.Close()
	}
	return err
}
