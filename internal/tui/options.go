package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// optionsModel lets the user set the install scope and sanitization passes
// before reviewing. Changes persist to preferences.
type optionsModel struct {
	cursor int
}

// optionRows is the fixed set of toggles, in display order.
const (
	optScope = iota
	optStripInvisible
	optStripComments
	optRowCount
)

func (s *optionsModel) enter(app *App) tea.Cmd { return nil }
func (s *optionsModel) capturing() bool        { return false }

func (s *optionsModel) update(app *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(k, app.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(k, app.keys.Down):
		if s.cursor < optRowCount-1 {
			s.cursor++
		}
	case key.Matches(k, app.keys.Confirm):
		return func() tea.Msg { return nav(stateConfirm) }
	case key.Matches(k, app.keys.Toggle), k.String() == "left", k.String() == "right", k.String() == "h", k.String() == "l":
		s.toggle(app, s.cursor)
	case key.Matches(k, app.keys.Back):
		return func() tea.Msg { return navBackMsg{} }
	}
	return nil
}

// toggle flips the option at row and persists the change.
func (s *optionsModel) toggle(app *App, row int) {
	switch row {
	case optScope:
		app.global = !app.global
		app.reresolveAgents()
	case optStripInvisible:
		app.security.StripInvisible = !app.security.StripInvisible
	case optStripComments:
		app.security.StripComments = !app.security.StripComments
	}
	app.saveScopeSanitize()
}

func (s *optionsModel) view(app *App) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", app.th.subtitle.Render("Scope & sanitization"))

	rows := []struct {
		label string
		value string
	}{
		{"Install scope", scopeValue(app)},
		{"Strip invisible characters", onOff(app.security.StripInvisible)},
		{"Strip comments (references)", onOff(app.security.StripComments)},
	}
	for i, r := range rows {
		cursor := "  "
		label := r.label
		if i == s.cursor {
			cursor = app.th.cursor.Render("▸ ")
			label = app.th.selected.Render(label)
		}
		fmt.Fprintf(&b, "%s%-32s %s\n", cursor, label, app.th.accent.Render(r.value))
	}

	b.WriteString("\n")
	b.WriteString(app.helpView(
		app.keys.Up, app.keys.Down,
		key.NewBinding(key.WithHelp("space", "toggle")),
		app.keys.Confirm, app.keys.Back,
	))
	return b.String()
}

func scopeValue(app *App) string {
	if app.global {
		return "global (~/)"
	}
	return "local (this project)"
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
