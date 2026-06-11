package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---- info list: pick a skill to inspect ------------------------------------

type infoListModel struct {
	ready     bool
	filter    textinput.Model
	filtering bool
	rows      []int // indices into app.discovered.Skills
	cursor    int
	offset    int
}

func (s *infoListModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.filter = textinput.New()
		s.filter.Prompt = "/ "
		s.filter.Placeholder = "filter skills"
		s.ready = true
	}
	s.rebuild(app)
	return nil
}

func (s *infoListModel) capturing() bool { return s.filtering }

func (s *infoListModel) rebuild(app *App) {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	s.rows = s.rows[:0]
	for i := range app.discovered.Skills {
		sk := &app.discovered.Skills[i]
		if q == "" || strings.Contains(strings.ToLower(sk.Name), q) ||
			strings.Contains(strings.ToLower(sk.Description), q) ||
			strings.Contains(strings.ToLower(sk.RelPath), q) {
			s.rows = append(s.rows, i)
		}
	}
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
}

func (s *infoListModel) update(app *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if s.filtering {
		switch k.String() {
		case "esc":
			s.filtering = false
			s.filter.Blur()
			s.filter.SetValue("")
			s.rebuild(app)
			return nil
		case "enter":
			s.filtering = false
			s.filter.Blur()
			return nil
		}
		var cmd tea.Cmd
		s.filter, cmd = s.filter.Update(k)
		s.rebuild(app)
		return cmd
	}

	switch {
	case key.Matches(k, app.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(k, app.keys.Down):
		if s.cursor < len(s.rows)-1 {
			s.cursor++
		}
	case key.Matches(k, app.keys.Filter):
		s.filtering = true
		return s.filter.Focus()
	case key.Matches(k, app.keys.Confirm):
		if s.cursor < len(s.rows) {
			app.infoSkill = &app.discovered.Skills[s.rows[s.cursor]]
			return func() tea.Msg { return nav(stateInfoDetail) }
		}
	case key.Matches(k, app.keys.Back):
		return app.quit()
	}
	return nil
}

func (s *infoListModel) view(app *App) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", app.th.subtitle.Render("Select a skill to inspect"))
	if s.filtering || s.filter.Value() != "" {
		fmt.Fprintf(&b, "%s\n", s.filter.View())
	}

	visible := app.bodyHeight() - 4
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
	if end > len(s.rows) {
		end = len(s.rows)
	}
	for i := s.offset; i < end; i++ {
		sk := app.discovered.Skills[s.rows[i]]
		cursor := "  "
		name := sk.Name
		if i == s.cursor {
			cursor = app.th.cursor.Render("▸ ")
			name = app.th.selected.Render(name)
		}
		fmt.Fprintf(&b, "%s%-26s %s\n", cursor, name, app.th.faint.Render(truncate(sk.Description, app.width-32)))
	}
	if len(s.rows) == 0 {
		fmt.Fprintf(&b, "%s\n", app.th.faint.Render("  (no matching skills)"))
	}

	b.WriteString("\n")
	if s.filtering {
		b.WriteString(app.helpView(app.keys.Up, app.keys.Down, app.keys.Confirm, app.keys.Back))
	} else {
		b.WriteString(app.helpView(app.keys.Up, app.keys.Down, app.keys.Filter, app.keys.Confirm, app.keys.Quit))
	}
	return b.String()
}

// ---- info detail: metadata + rendered markdown -----------------------------

type infoDetailModel struct {
	ready    bool
	preview  viewport.Model
	focused  bool
	glam     *glamour.TermRenderer
	previewW int
	gen      int
}

func (s *infoDetailModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.preview = viewport.New(0, 0)
		s.ready = true
	}
	s.focused = false
	s.layout(app)
	s.preview.SetContent(app.th.faint.Render("loading…"))
	s.preview.GotoTop()
	return s.loadDoc(app)
}

func (s *infoDetailModel) capturing() bool { return false }

func (s *infoDetailModel) leftWidth(app *App) int {
	w := app.width * 2 / 5
	if w < 20 {
		w = 20
	}
	return w
}

func (s *infoDetailModel) layout(app *App) {
	leftW := s.leftWidth(app)
	rightW := app.width - leftW - 4
	if rightW < 10 {
		rightW = 10
	}
	// Reserve a line for the help legend below the panes: the bordered preview is
	// preview.Height+2 tall, and the view appends "\n"+help, so the outer pane
	// must be one shorter than the body height or the legend gets clipped.
	h := app.bodyHeight() - 3
	if h < 1 {
		h = 1
	}
	s.preview.Width = rightW
	s.preview.Height = h
	if s.previewW != rightW || s.glam == nil {
		s.previewW = rightW
		if r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(rightW)); err == nil {
			s.glam = r
		}
	}
}

func (s *infoDetailModel) loadDoc(app *App) tea.Cmd {
	if app.infoSkill == nil {
		return nil
	}
	s.gen++
	gen := s.gen
	sk := *app.infoSkill
	eng := app.eng
	return func() tea.Msg {
		doc, err := eng.ReadSkillDoc(sk)
		return skillDocMsg{gen: gen, doc: doc, err: err, path: sk.RelPath}
	}
}

func (s *infoDetailModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.layout(app)
		return nil
	case skillDocMsg:
		if msg.gen != s.gen {
			return nil
		}
		if msg.err != nil {
			s.preview.SetContent(app.th.faint.Render("(no preview: " + msg.err.Error() + ")"))
			return nil
		}
		body := msg.doc.Body
		if s.glam != nil {
			if out, err := s.glam.Render(body); err == nil {
				body = out
			}
		}
		// Prepend the raw frontmatter in a bordered box so it reads as a distinct
		// block atop the rendered doc — glamour's own code-fence background is
		// nearly invisible on a dark terminal, so we draw an explicit border.
		if fm := strings.TrimSpace(msg.doc.Skill.FrontmatterRaw); fm != "" {
			body = frontmatterBox(app, fm, s.preview.Width) + "\n\n" + body
		}
		s.preview.SetContent(body)
		s.preview.GotoTop()
		return nil
	case tea.KeyMsg:
		if s.focused {
			switch msg.String() {
			case "tab", "esc", "left", "h":
				s.focused = false
				return nil
			}
			var cmd tea.Cmd
			s.preview, cmd = s.preview.Update(msg)
			return cmd
		}
		switch {
		case msg.String() == "tab":
			s.focused = true
		case key.Matches(msg, app.keys.Back):
			if len(app.discovered.Skills) > 1 {
				return func() tea.Msg { return nav(stateInfoList) }
			}
			return app.quit()
		}
	}
	return nil
}

func (s *infoDetailModel) view(app *App) string {
	leftW := s.leftWidth(app)

	borderStyle := app.th.border
	if s.focused {
		borderStyle = app.th.borderFocus
	}
	right := borderStyle.Render(s.preview.View())
	// Cap the metadata column to the bordered preview's height (Height+2 for the
	// border) so long frontmatter can't push the help legend off-screen.
	left := lipgloss.NewStyle().Width(leftW).MaxHeight(s.preview.Height + 2).Render(s.metadata(app, leftW))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	var help string
	if s.focused {
		help = app.helpView(app.keys.Up, app.keys.Down, key.NewBinding(key.WithHelp("esc", "back to metadata")))
	} else {
		back := "quit"
		if len(app.discovered.Skills) > 1 {
			back = "list"
		}
		help = app.helpView(
			key.NewBinding(key.WithHelp("tab", "scroll doc")),
			key.NewBinding(key.WithHelp("esc", back)),
		)
	}
	return body + "\n" + help
}

func (s *infoDetailModel) metadata(app *App, w int) string {
	sk := app.infoSkill
	if sk == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", app.th.title.Render(sk.Name))

	src := app.discovered.Source
	if app.discovered.Ref != "" {
		src += "@" + app.discovered.Ref
	}
	fmt.Fprintf(&b, "%s\n", app.th.faint.Render(src))
	if sk.RelPath != "" {
		fmt.Fprintf(&b, "%s\n", app.th.faint.Render(sk.RelPath))
	}
	if sk.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", lipgloss.NewStyle().Width(w).Render(sk.Description))
	}
	// The raw frontmatter lives in the preview pane on the right (see the
	// skillDocMsg handler), not here — keep the metadata column compact.
	return b.String()
}

// frontmatterBox renders raw skill frontmatter inside a bordered box, with keys
// highlighted, so it's clearly delimited from the rendered document below it.
// previewW is the width of the (bordered) preview viewport it sits in. Shared by
// the info detail screen and the install picker's preview pane.
func frontmatterBox(app *App, fm string, previewW int) string {
	var lines []string
	for _, ln := range strings.Split(fm, "\n") {
		trimmed := strings.TrimLeft(ln, " \t")
		// Color "key:" but leave list items, comments and continuations plain.
		if i := strings.Index(ln, ":"); i > 0 && trimmed != "" &&
			!strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "-") {
			lines = append(lines, app.th.accent.Render(ln[:i+1])+ln[i+1:])
		} else {
			lines = append(lines, ln)
		}
	}
	w := previewW - 2 // leave room for the box's left/right border
	if w < 10 {
		w = 10
	}
	return app.th.border.Width(w).Render(strings.Join(lines, "\n"))
}
