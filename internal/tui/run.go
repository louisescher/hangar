package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the interactive TUI and blocks until the user exits. The
// discovered source (and its temp extraction dir) is released on exit.
func Run(opt Options) error {
	m := New(opt)
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
