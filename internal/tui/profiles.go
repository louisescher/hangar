package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/lockfile"
)

// ---- profile list: pick a saved profile to inspect -------------------------

type profileListModel struct {
	names         []string
	counts        map[string]int
	cursor        int
	offset        int
	confirmRemove string // profile pending delete confirmation
	note          string
}

func (s *profileListModel) enter(app *App) tea.Cmd {
	s.confirmRemove = ""
	s.reload(app)
	return nil
}

func (s *profileListModel) reload(app *App) {
	names, err := config.ListProfiles()
	if err != nil {
		app.err = err
		return
	}
	s.names = names
	s.counts = make(map[string]int, len(names))
	for _, n := range names {
		if p, err := config.LoadProfile(n); err == nil {
			s.counts[n] = len(p.Skills)
		}
	}
	if s.cursor >= len(s.names) {
		s.cursor = max(0, len(s.names)-1)
	}
}

func (s *profileListModel) capturing() bool { return s.confirmRemove != "" }

func (s *profileListModel) update(app *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	if s.confirmRemove != "" {
		switch k.String() {
		case "y":
			name := s.confirmRemove
			s.confirmRemove = ""
			if _, err := config.RemoveProfile(name); err != nil {
				s.note = "error: " + err.Error()
			} else {
				s.note = "removed profile " + name
			}
			s.reload(app)
		default:
			s.confirmRemove = ""
			s.note = ""
		}
		return nil
	}

	s.note = ""
	switch {
	case key.Matches(k, app.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(k, app.keys.Down):
		if s.cursor < len(s.names)-1 {
			s.cursor++
		}
	case key.Matches(k, app.keys.Confirm):
		if s.cursor < len(s.names) {
			app.profileName = s.names[s.cursor]
			return func() tea.Msg { return nav(stateProfileDetail) }
		}
	case k.String() == "x" || k.String() == "d":
		if s.cursor < len(s.names) {
			s.confirmRemove = s.names[s.cursor]
		}
	case key.Matches(k, app.keys.Back):
		return func() tea.Msg { return navBackMsg{} }
	}
	return nil
}

func (s *profileListModel) view(app *App) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", app.th.subtitle.Render("Saved profiles"))

	if len(s.names) == 0 {
		fmt.Fprintf(&b, "%s\n\n", app.th.faint.Render("  no saved profiles — run `hangar profile save <name>`"))
		b.WriteString(app.helpView(app.keys.Quit))
		return b.String()
	}

	visible := app.bodyHeight() - 5
	if visible < 1 {
		visible = 1
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+visible {
		s.offset = s.cursor - visible + 1
	}
	end := s.offset + visible
	if end > len(s.names) {
		end = len(s.names)
	}
	for i := s.offset; i < end; i++ {
		name := s.names[i]
		cursor := "  "
		label := name
		if i == s.cursor {
			cursor = app.th.cursor.Render("▸ ")
			label = app.th.selected.Render(name)
		}
		fmt.Fprintf(&b, "%s%-28s %s\n", cursor, label, app.th.faint.Render(fmt.Sprintf("%d entries", s.counts[name])))
	}

	b.WriteString("\n")
	switch {
	case s.confirmRemove != "":
		fmt.Fprintf(&b, "%s%s\n", app.th.errorText.Render("delete profile "+s.confirmRemove+"? "), app.th.faint.Render("(y to confirm, n to cancel)"))
	case s.note != "":
		fmt.Fprintf(&b, "%s\n", app.th.okText.Render(s.note))
	default:
		b.WriteString("\n")
	}
	b.WriteString(app.helpView(
		app.keys.Up, app.keys.Down, app.keys.Confirm,
		key.NewBinding(key.WithHelp("x", "delete profile")),
		app.keys.Quit,
	))
	return b.String()
}

// ---- profile detail: inspect & prune a profile's entries -------------------

type profileDetailModel struct {
	entries       []lockfile.Entry
	cursor        int
	offset        int
	confirmRemove string // entry name pending removal
	note          string
}

func (s *profileDetailModel) enter(app *App) tea.Cmd {
	s.confirmRemove = ""
	s.note = ""
	p, err := config.LoadProfile(app.profileName)
	if err != nil {
		app.err = err
		return nil
	}
	s.entries = p.Skills
	if s.cursor >= len(s.entries) {
		s.cursor = max(0, len(s.entries)-1)
	}
	return nil
}

func (s *profileDetailModel) capturing() bool { return s.confirmRemove != "" }

func (s *profileDetailModel) cur() *lockfile.Entry {
	if s.cursor < 0 || s.cursor >= len(s.entries) {
		return nil
	}
	return &s.entries[s.cursor]
}

func (s *profileDetailModel) update(app *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	if s.confirmRemove != "" {
		switch k.String() {
		case "y":
			name := s.confirmRemove
			s.confirmRemove = ""
			return s.removeEntry(app, name)
		default:
			s.confirmRemove = ""
			s.note = ""
		}
		return nil
	}

	s.note = ""
	switch {
	case key.Matches(k, app.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(k, app.keys.Down):
		if s.cursor < len(s.entries)-1 {
			s.cursor++
		}
	case k.String() == "x" || k.String() == "d":
		if e := s.cur(); e != nil {
			s.confirmRemove = e.Name
		}
	case key.Matches(k, app.keys.Back):
		return func() tea.Msg { return navBackMsg{} }
	}
	return nil
}

// removeEntry drops one entry from the profile on disk. When the last entry is
// removed the profile is deleted and we return to the list.
func (s *profileDetailModel) removeEntry(app *App, name string) tea.Cmd {
	p, err := config.LoadProfile(app.profileName)
	if err != nil {
		s.note = "error: " + err.Error()
		return nil
	}
	kept := make([]lockfile.Entry, 0, len(p.Skills))
	for _, e := range p.Skills {
		if e.Name != name {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		if _, err := config.RemoveProfile(app.profileName); err != nil {
			s.note = "error: " + err.Error()
			return nil
		}
		return func() tea.Msg { return navBackMsg{} } // profile is now empty/gone
	}
	p.Skills = kept
	if err := config.SaveProfile(p); err != nil {
		s.note = "error: " + err.Error()
		return nil
	}
	s.entries = kept
	if s.cursor >= len(s.entries) {
		s.cursor = max(0, len(s.entries)-1)
	}
	s.note = "removed " + name
	return nil
}

func (s *profileDetailModel) view(app *App) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", app.th.subtitle.Render("Profile — "+app.profileName))

	if len(s.entries) == 0 {
		fmt.Fprintf(&b, "%s\n\n", app.th.faint.Render("  (empty)"))
		b.WriteString(app.helpView(app.keys.Back, app.keys.Quit))
		return b.String()
	}

	// Reserve room for the details panel (5), status (1), help (1).
	visible := app.bodyHeight() - 11
	if visible < 1 {
		visible = 1
	}
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+visible {
		s.offset = s.cursor - visible + 1
	}
	end := s.offset + visible
	if end > len(s.entries) {
		end = len(s.entries)
	}
	for i := s.offset; i < end; i++ {
		e := s.entries[i]
		cursor := "  "
		name := e.Name
		if i == s.cursor {
			cursor = app.th.cursor.Render("▸ ")
			name = app.th.selected.Render(name)
		}
		kind := "skill"
		if e.Kind == lockfile.KindRef {
			kind = "ref"
		}
		fmt.Fprintf(&b, "%s%-24s %s\n", cursor, name, app.th.faint.Render(kind+"  "+sourceRefLabel(e)))
	}

	// Details panel for the cursor entry.
	if e := s.cur(); e != nil {
		fmt.Fprintf(&b, "\n%s\n", app.th.accent.Render("details"))
		fmt.Fprintf(&b, "  %s %s\n", app.th.faint.Render("source:"), sourceRefLabel(*e))
		kind := "skill"
		if e.Kind == lockfile.KindRef {
			kind = "ref"
		}
		fmt.Fprintf(&b, "  %s %s\n", app.th.faint.Render("kind:  "), kind)
		if e.Subpath != "" {
			fmt.Fprintf(&b, "  %s %s\n", app.th.faint.Render("path:  "), e.Subpath)
		}
		if e.Pinned {
			fmt.Fprintf(&b, "  %s\n", app.th.accent.Render("pinned"))
		}
	}

	b.WriteString("\n")
	switch {
	case s.confirmRemove != "":
		fmt.Fprintf(&b, "%s%s\n", app.th.errorText.Render("remove "+s.confirmRemove+" from this profile? "), app.th.faint.Render("(y/n)"))
	case s.note != "":
		fmt.Fprintf(&b, "%s\n", app.th.okText.Render(s.note))
	default:
		b.WriteString("\n")
	}
	b.WriteString(app.helpView(
		app.keys.Up, app.keys.Down,
		key.NewBinding(key.WithHelp("x", "remove entry")),
		app.keys.Back, app.keys.Quit,
	))
	return b.String()
}

// sourceRefLabel renders "source@ref" (or "@version" for npm) for an entry.
func sourceRefLabel(e lockfile.Entry) string {
	src := e.Source
	if v := entryRefLabel(e); v != "" {
		src += "@" + v
	}
	return src
}
