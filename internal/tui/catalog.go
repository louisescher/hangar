package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/louisescher/hangar/internal/index"
	"github.com/louisescher/hangar/internal/spec"
)

// catalogModel is the browse screen: a filterable list of curated sources plus
// a "/" filter that doubles as free-form source entry (owner/repo, npm:pkg, …).
type catalogModel struct {
	input     textinput.Model
	all       []index.Entry
	filtered  []index.Entry
	cursor    int
	filtering bool
	ready     bool
}

func (s *catalogModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.input = textinput.New()
		s.input.Placeholder = "filter the catalog, or type a source like owner/repo or npm:pkg"
		s.input.Prompt = "/ "
		s.all = app.eng.Index()
		s.filtered = s.all
		s.ready = true
	}
	return nil
}

func (s *catalogModel) capturing() bool { return s.filtering }

func (s *catalogModel) applyFilter() {
	q := strings.TrimSpace(s.input.Value())
	s.filtered = index.Search(q)
	if s.cursor >= len(s.filtered) {
		s.cursor = max(0, len(s.filtered)-1)
	}
}

func (s *catalogModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.filtering {
			switch msg.String() {
			case "esc":
				s.filtering = false
				s.input.Blur()
				s.input.SetValue("")
				s.applyFilter()
				return nil
			case "enter":
				return s.choose(app)
			case "up", "down":
				s.moveCursor(msg.String())
				return nil
			}
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			s.applyFilter()
			return cmd
		}

		switch {
		case key.Matches(msg, app.keys.Filter):
			s.filtering = true
			return s.input.Focus()
		case key.Matches(msg, app.keys.Up):
			s.moveCursor("up")
		case key.Matches(msg, app.keys.Down):
			s.moveCursor("down")
		case key.Matches(msg, app.keys.Confirm):
			return s.choose(app)
		}
	}
	return nil
}

func (s *catalogModel) moveCursor(dir string) {
	if dir == "up" && s.cursor > 0 {
		s.cursor--
	}
	if dir == "down" && s.cursor < len(s.filtered)-1 {
		s.cursor++
	}
}

// choose installs the highlighted catalog entry, or — when the filter text has
// no catalog match — parses it as a free-form source spec.
func (s *catalogModel) choose(app *App) tea.Cmd {
	var raw string
	switch {
	case len(s.filtered) > 0:
		raw = s.filtered[s.cursor].Spec
	case strings.TrimSpace(s.input.Value()) != "":
		raw = strings.TrimSpace(s.input.Value())
	default:
		return nil
	}
	sp, err := spec.Parse(raw)
	if err != nil {
		app.err = err
		return nil
	}
	app.src = sp
	return func() tea.Msg { return nav(stateCrawling) }
}

// catRow is one rendered line of the catalog: a category header (entry == -1)
// or a catalog entry (entry indexes s.filtered).
type catRow struct {
	header string
	entry  int
}

func (s *catalogModel) renderRows() []catRow {
	var rows []catRow
	last := ""
	for i := range s.filtered {
		cat := s.filtered[i].Category
		if cat == "" {
			cat = "Other"
		}
		if cat != last {
			rows = append(rows, catRow{entry: -1, header: cat})
			last = cat
		}
		rows = append(rows, catRow{entry: i})
	}
	return rows
}

func (s *catalogModel) view(app *App) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", app.th.subtitle.Render("Browse skills by category — or type your own source."))
	fmt.Fprintf(&b, "%s\n\n", s.input.View())

	if len(s.filtered) == 0 {
		b.WriteString(app.th.faint.Render("  no catalog match — press ⏎ to fetch this source\n"))
		fmt.Fprintf(&b, "\n%s", app.helpView(app.keys.Confirm, app.keys.Back))
		return b.String()
	}

	rows := s.renderRows()
	// Locate the cursor's render row to keep it on screen.
	cursorRow := 0
	for i, r := range rows {
		if r.entry == s.cursor {
			cursorRow = i
			break
		}
	}
	visible := app.bodyHeight() - 6
	if visible < 1 {
		visible = 1
	}
	start := 0
	if cursorRow >= visible {
		start = cursorRow - visible + 1
	}
	end := start + visible
	if end > len(rows) {
		end = len(rows)
	}

	for i := start; i < end; i++ {
		r := rows[i]
		if r.entry == -1 {
			fmt.Fprintf(&b, "%s\n", app.th.badge.Render(r.header))
			continue
		}
		e := s.filtered[r.entry]
		cursor := "  "
		name := e.Name
		if r.entry == s.cursor {
			cursor = app.th.cursor.Render("▸ ")
			name = app.th.selected.Render(name)
		}
		b.WriteString(fmt.Sprintf("  %s%-24s %s\n", cursor, name, app.th.faint.Render(truncate(e.Description, app.width-34))))
	}

	help := "\n"
	if s.filtering {
		help += app.helpView(app.keys.Up, app.keys.Down, app.keys.Confirm, app.keys.Back)
	} else {
		help += app.helpView(app.keys.Up, app.keys.Down, app.keys.Confirm, app.keys.Filter, app.keys.Quit)
	}
	b.WriteString(help)
	return b.String()
}
