package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/louisescher/hangar/internal/spec"
)

const asciiLogo = `          \            /               ██╗  ██╗  █████╗  ███╗   ██╗  ██████╗   █████╗  ██████╗
           \    __    /                ██║  ██║ ██╔══██╗ ████╗  ██║ ██╔════╝  ██╔══██╗ ██╔══██╗
____________\.-|__|-./____________     ███████║ ███████║ ██╔██╗ ██║ ██║  ███╗ ███████║ ██████╔╝
    + + ---\__| \/ |__/--- + +         ██╔══██║ ██╔══██║ ██║╚██╗██║ ██║   ██║ ██╔══██║ ██╔══██╗
               \__/                    ██║  ██║ ██║  ██║ ██║ ╚████║  ██████╔╝ ██║  ██║ ██║  ██║
                                       ╚═╝  ╚═╝ ╚═╝  ╚═╝ ╚═╝  ╚═══╝  ╚═════╝  ╚═╝  ╚═╝ ╚═╝  ╚═╝`

// asciiLogoText is the text-only fallback used when the terminal is too narrow
// to display the full logo (plane + text side-by-side).
const asciiLogoText = ` ██╗  ██╗  █████╗  ███╗   ██╗  ██████╗   █████╗  ██████╗
 ██║  ██║ ██╔══██╗ ████╗  ██║ ██╔════╝  ██╔══██╗ ██╔══██╗
 ███████║ ███████║ ██╔██╗ ██║ ██║  ███╗ ███████║ ██████╔╝
 ██╔══██║ ██╔══██║ ██║╚██╗██║ ██║   ██║ ██╔══██║ ██╔══██╗
 ██║  ██║ ██║  ██║ ██║ ╚████║  ██████╔╝ ██║  ██║ ██║  ██║
 ╚═╝  ╚═╝ ╚═╝  ╚═╝ ╚═╝  ╚═══╝  ╚═════╝  ╚═╝  ╚═╝ ╚═╝  ╚═╝`

// catalogModel is the home screen: a search bar where the user types any
// source spec (owner/repo, npm:pkg, …) and presses ⏎ to install.
type catalogModel struct {
	input textinput.Model
	ready bool
}

func (s *catalogModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.input = textinput.New()
		s.input.Placeholder = "owner/repo  ·  owner/repo/subpath  ·  https://github.com/owner/repo  ·  npm:package"
		s.input.Prompt = "  "
		s.ready = true
	}
	s.input.Width = s.inputContentWidth(app.width)
	return s.input.Focus()
}

// capturing always returns true so the catalog's update handles all keystrokes
// directly, including q-to-quit.
func (s *catalogModel) capturing() bool { return true }

func (s *catalogModel) inputContentWidth(termWidth int) int {
	w := termWidth - 12 // 4-char margin + 1 border + 1 padding on each side
	if w < 20 {
		return 20
	}
	return w
}

func (s *catalogModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if strings.TrimSpace(s.input.Value()) != "" {
				s.input.SetValue("")
				return nil
			}
			return func() tea.Msg { return navBackMsg{} }
		case "enter":
			return s.choose(app)
		}
		// q quits when the input is empty; otherwise it types into the search box.
		if key.Matches(msg, app.keys.Quit) && strings.TrimSpace(s.input.Value()) == "" {
			return func() tea.Msg { return navBackMsg{} }
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return cmd
	case tea.WindowSizeMsg:
		s.input.Width = s.inputContentWidth(msg.Width)
	}
	return nil
}

func (s *catalogModel) choose(app *App) tea.Cmd {
	raw := strings.TrimSpace(s.input.Value())
	if raw == "" {
		app.err = fmt.Errorf("type a source to install, e.g. owner/repo or npm:package")
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

func (s *catalogModel) view(app *App) string {
	return s.homeView(app)
}

func (s *catalogModel) homeView(app *App) string {
	accent := lipgloss.AdaptiveColor{Light: "#5b21b6", Dark: "#a78bfa"}
	faint := lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	accentStyle := lipgloss.NewStyle().Foreground(accent)
	faintStyle := lipgloss.NewStyle().Foreground(faint)

	// Center a single-line string by its visual width.
	centerLine := func(s string) string {
		pad := max(0, (app.width-lipgloss.Width(s))/2)
		return strings.Repeat(" ", pad) + s
	}

	// Stable logo margin: compute once from the widest line so every row of the
	// block shares the same left offset. Per-line lipgloss centering causes
	// horizontal drift at certain terminal widths.
	//
	// Use the text-only fallback when the terminal is too narrow to fit the full
	// combined logo (plane + text) with a comfortable margin.
	logoLines := strings.Split(asciiLogo, "\n")
	logoWidth := 0
	for _, l := range logoLines {
		if w := lipgloss.Width(l); w > logoWidth {
			logoWidth = w
		}
	}
	if app.width < logoWidth+4 {
		logoLines = strings.Split(asciiLogoText, "\n")
		logoWidth = 0
		for _, l := range logoLines {
			if w := lipgloss.Width(l); w > logoWidth {
				logoWidth = w
			}
		}
	}
	logoPad := strings.Repeat(" ", max(0, (app.width-logoWidth)/2))

	logoHeight := len(logoLines)
	// logo + blank + tagline + blank + search(3) + blank + examples + blank + help
	contentHeight := logoHeight + 1 + 1 + 1 + 3 + 1 + 1 + 1 + 1
	topPad := max(1, (app.bodyHeight()-contentHeight)/2)

	var b strings.Builder
	b.WriteString(strings.Repeat("\n", topPad))

	// Logo — every line shares the same left margin
	for _, l := range logoLines {
		b.WriteString(logoPad + accentStyle.Render(l) + "\n")
	}
	b.WriteString("\n")

	// Tagline
	b.WriteString(centerLine(faintStyle.Render("a TUI package manager for AI agent skills")) + "\n")
	b.WriteString("\n")

	// Search box
	b.WriteString(s.searchBarView(app) + "\n")
	b.WriteString("\n")

	// Example specs
	b.WriteString(centerLine(faintStyle.Render("e.g.  anthropics/skills  ·  owner/repo/subpath  ·  https://github.com/owner/repo  ·  npm:@scope/pkg")) + "\n")
	b.WriteString("\n")

	// Help
	b.WriteString(centerLine(app.helpView(app.keys.Confirm, app.keys.Quit)))

	return b.String()
}

func (s *catalogModel) searchBarView(app *App) string {
	iw := s.inputContentWidth(app.width)
	s.input.Width = iw

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#5b21b6", Dark: "#a78bfa"}).
		Padding(0, 1).
		Width(iw).
		Render(s.input.View())

	return lipgloss.NewStyle().Width(app.width).Align(lipgloss.Center).Render(box)
}
