package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// confirmModel is the review screen: a final summary that also lets the user
// tweak scope/sanitization inline, jump back to a specific step, and page
// through the full skill list.
type confirmModel struct {
	showAll bool
	list    viewport.Model
	ready   bool
}

func (s *confirmModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.list = viewport.New(0, 0)
		s.ready = true
	}
	s.showAll = false
	return nil
}

func (s *confirmModel) capturing() bool { return false }

func (s *confirmModel) update(app *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	// While paging the full list, arrows scroll it.
	if s.showAll {
		switch k.String() {
		case "f", "esc":
			s.showAll = false
			return nil
		}
		var cmd tea.Cmd
		s.list, cmd = s.list.Update(k)
		return cmd
	}

	switch {
	case key.Matches(k, app.keys.Confirm):
		return func() tea.Msg { return nav(stateInstalling) }
	case key.Matches(k, app.keys.Back):
		return func() tea.Msg { return navBackMsg{} }
	case k.String() == "a":
		return func() tea.Msg { return nav(stateAgents) }
	case k.String() == "s":
		return func() tea.Msg { return nav(stateTree) }
	case k.String() == "o":
		return func() tea.Msg { return nav(stateOptions) }
	case k.String() == "g":
		app.global = !app.global
		app.reresolveAgents()
		app.saveScopeSanitize()
	case k.String() == "i":
		app.security.StripInvisible = !app.security.StripInvisible
		app.saveScopeSanitize()
	case k.String() == "c":
		app.security.StripComments = !app.security.StripComments
		app.saveScopeSanitize()
	case k.String() == "f":
		s.openFullList(app)
	}
	return nil
}

func (s *confirmModel) openFullList(app *App) {
	var names []string
	for _, sk := range app.selectedSkills() {
		names = append(names, "• "+sk.Name)
	}
	if len(names) == 0 {
		names = []string{app.th.faint.Render("(no skills selected)")}
	}
	s.list.Width = maxInt(app.width-4, 10)
	s.list.Height = maxInt(app.bodyHeight()-3, 3)
	s.list.SetContent(strings.Join(names, "\n"))
	s.list.GotoTop()
	s.showAll = true
}

func (s *confirmModel) view(app *App) string {
	if s.showAll {
		var b strings.Builder
		skills := app.selectedSkills()
		fmt.Fprintf(&b, "%s\n", app.th.title.Render(fmt.Sprintf("Selected skills (%d)", len(skills))))
		fmt.Fprintf(&b, "%s\n", app.th.border.Render(s.list.View()))
		b.WriteString(app.helpView(
			app.keys.Up, app.keys.Down,
			key.NewBinding(key.WithHelp("f/esc", "back to review")),
		))
		return b.String()
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", app.th.title.Render("Review"))

	skills := app.selectedSkills()
	fmt.Fprintf(&b, "%s  %s\n", app.th.accent.Render(fmt.Sprintf("Skills (%d):", len(skills))), app.th.faint.Render("press f for the full list"))
	for i, sk := range skills {
		if i >= 6 {
			fmt.Fprintf(&b, "%s\n", app.th.faint.Render(fmt.Sprintf("  … and %d more (f)", len(skills)-6)))
			break
		}
		fmt.Fprintf(&b, "  • %s\n", sk.Name)
	}
	if len(skills) == 0 {
		b.WriteString(app.th.faint.Render("  (none)\n"))
	}

	if n := len(app.discovered.References); n > 0 {
		fmt.Fprintf(&b, "\n%s %s\n",
			app.th.accent.Render(fmt.Sprintf("References (%d):", n)),
			app.th.faint.Render("README/docs added as agent references"))
	}

	fmt.Fprintf(&b, "\n%s ", app.th.accent.Render(fmt.Sprintf("Agents (%d):", len(app.chosen))))
	if len(app.chosen) == 0 {
		b.WriteString(app.th.faint.Render("(none — skills go to .agents/skills only)"))
	} else {
		var names []string
		for _, a := range app.chosen {
			names = append(names, a.Def.Display)
		}
		b.WriteString(strings.Join(names, ", "))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "\n%s %s\n", app.th.faint.Render("scope:"), app.th.accent.Render(scopeValue(app)))
	fmt.Fprintf(&b, "%s %s\n", app.th.faint.Render("sanitize:"), app.th.accent.Render(sanitizeLabel(app)))

	b.WriteString("\n")
	b.WriteString(app.helpView(
		app.keys.Confirm,
		key.NewBinding(key.WithHelp("f", "full list")),
		key.NewBinding(key.WithHelp("s", "skills")),
		key.NewBinding(key.WithHelp("a", "agents")),
		key.NewBinding(key.WithHelp("o", "options")),
		key.NewBinding(key.WithHelp("g/i/c", "scope/sanitize")),
		app.keys.Back,
	))
	return b.String()
}

func sanitizeLabel(app *App) string {
	var on []string
	if app.security.StripInvisible {
		on = append(on, "invisible chars")
	}
	if app.security.StripComments {
		on = append(on, "comments (refs)")
	}
	if len(on) == 0 {
		return "off"
	}
	return strings.Join(on, ", ")
}
