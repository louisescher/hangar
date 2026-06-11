package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/louisescher/hangar/internal/agents"
)

// agentsModel picks which agents to install into. Detected agents are flagged
// and pre-selected.
type agentsModel struct {
	defs     []agents.Def
	detected map[string]bool
	sel      map[string]bool
	cursor   int
	offset   int
	seeded   bool
}

func (s *agentsModel) enter(app *App) tea.Cmd {
	if s.sel == nil {
		s.sel = map[string]bool{}
	}
	det, _ := app.eng.DetectAgents(app.global)
	s.detected = map[string]bool{}
	for _, a := range det {
		s.detected[a.Def.Name] = true
	}
	// Order: detected first, then the rest, each alphabetical by display name.
	s.defs = append([]agents.Def(nil), app.eng.AgentDefs()...)
	sort.SliceStable(s.defs, func(i, j int) bool {
		di, dj := s.detected[s.defs[i].Name], s.detected[s.defs[j].Name]
		if di != dj {
			return di
		}
		return s.defs[i].Display < s.defs[j].Display
	})

	if !s.seeded {
		pre := app.agents
		if len(pre) == 0 {
			for n := range s.detected {
				pre = append(pre, n)
			}
		}
		if len(pre) == 0 {
			pre = app.prefs.DefaultAgents
		}
		for _, n := range pre {
			if _, _, ok := agents.FindDef(n); ok {
				s.sel[canonicalName(n)] = true
			}
		}
		s.seeded = true
	}
	return nil
}

func (s *agentsModel) capturing() bool { return false }

func (s *agentsModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, app.keys.Up):
			if s.cursor > 0 {
				s.cursor--
			}
			s.clamp(app)
		case key.Matches(msg, app.keys.Down):
			if s.cursor < len(s.defs)-1 {
				s.cursor++
			}
			s.clamp(app)
		case key.Matches(msg, app.keys.Toggle):
			name := s.defs[s.cursor].Name
			s.sel[name] = !s.sel[name]
		case key.Matches(msg, app.keys.All):
			for _, d := range s.defs {
				s.sel[d.Name] = true
			}
		case key.Matches(msg, app.keys.None):
			s.sel = map[string]bool{}
		case msg.String() == "d":
			// Select exactly the detected agents (deselect everything else).
			s.sel = map[string]bool{}
			for name := range s.detected {
				s.sel[name] = true
			}
		case key.Matches(msg, app.keys.Confirm):
			return s.confirm(app)
		case key.Matches(msg, app.keys.Back):
			return func() tea.Msg { return navBackMsg{} }
		}
	}
	return nil
}

func (s *agentsModel) confirm(app *App) tea.Cmd {
	var names []string
	for _, d := range s.defs {
		if s.sel[d.Name] {
			names = append(names, d.Name)
		}
	}
	agts, err := app.eng.ResolveAgents(names, app.global)
	if err != nil {
		app.err = err
		return nil
	}
	app.chosen = agts
	return func() tea.Msg { return nav(stateOptions) }
}

func (s *agentsModel) clamp(app *App) {
	visible := s.height(app)
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+visible {
		s.offset = s.cursor - visible + 1
	}
}

func (s *agentsModel) height(app *App) int {
	// Reserve rows for the subtitle (2), the selected-count badge (2), and the
	// help legend (1) so the legend is never clipped by the frame.
	h := app.bodyHeight() - 5
	if h < 1 {
		h = 1
	}
	return h
}

func (s *agentsModel) view(app *App) string {
	var b strings.Builder
	chosen := 0
	for _, v := range s.sel {
		if v {
			chosen++
		}
	}
	fmt.Fprintf(&b, "%s\n\n", app.th.subtitle.Render("Choose target agents (detected ones are pre-selected)."))

	visible := s.height(app)
	end := s.offset + visible
	if end > len(s.defs) {
		end = len(s.defs)
	}
	for i := s.offset; i < end; i++ {
		d := s.defs[i]
		box := "[ ]"
		if s.sel[d.Name] {
			box = app.th.check.Render("[x]")
		}
		cursor := "  "
		label := d.Display
		if i == s.cursor {
			cursor = app.th.cursor.Render("▸ ")
			label = app.th.selected.Render(label)
		}
		flag := ""
		if s.detected[d.Name] {
			flag = app.th.okText.Render(" ✓ detected")
		}
		b.WriteString(fmt.Sprintf("%s%s %s%s\n", cursor, box, label, flag))
	}

	fmt.Fprintf(&b, "\n%s\n", app.th.badge.Render(fmt.Sprintf("%d agent(s) selected", chosen)))
	b.WriteString(app.helpView(
		app.keys.Up, app.keys.Down, app.keys.Toggle,
		app.keys.All, app.keys.None,
		key.NewBinding(key.WithHelp("d", "detected only")),
		app.keys.Confirm, app.keys.Back,
	))
	return b.String()
}

// canonicalName resolves an alias to its canonical agent name.
func canonicalName(name string) string {
	if def, _, ok := agents.FindDef(name); ok {
		return def.Name
	}
	return name
}
