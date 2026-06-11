package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/louisescher/hangar/internal/lockfile"
)

// resultsModel shows the install outcome and any audit findings.
type resultsModel struct{}

func (s *resultsModel) enter(app *App) tea.Cmd { return nil }
func (s *resultsModel) capturing() bool        { return false }

func (s *resultsModel) update(app *App, msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(k, app.keys.Back):
			// Install something else: drop the finished source and browse again.
			if app.discovered != nil {
				_ = app.discovered.Close()
				app.discovered = nil
			}
			app.report = nil
			app.selection = newSkillSet()
			return func() tea.Msg { return nav(stateCatalog) }
		case key.Matches(k, app.keys.Confirm):
			return app.quit()
		}
	}
	return nil
}

func (s *resultsModel) view(app *App) string {
	var b strings.Builder
	if app.err != nil {
		fmt.Fprintf(&b, "%s\n\n", app.th.errorText.Render("install failed"))
		fmt.Fprintf(&b, "%s\n\n", app.th.faint.Render("  "+app.err.Error()))
		b.WriteString(app.helpView(app.keys.Back, app.keys.Confirm))
		return b.String()
	}
	rep := app.report
	if rep == nil {
		b.WriteString(app.th.faint.Render("nothing was installed\n"))
		return b.String()
	}

	fmt.Fprintf(&b, "%s\n\n", app.th.okText.Render("✓ done"))

	var skills, refs []string
	for _, sr := range rep.Skills {
		if sr.Kind == lockfile.KindRef {
			refs = append(refs, sr.Name)
		} else {
			skills = append(skills, sr.Name)
		}
	}
	if len(skills) > 0 {
		fmt.Fprintf(&b, "%s%s\n", app.th.accent.Render("Installed skills: "), strings.Join(skills, ", "))
	}
	if len(refs) > 0 {
		fmt.Fprintf(&b, "%s%s\n", app.th.accent.Render("Added references: "), strings.Join(refs, ", "))
	}

	agts := append([]string(nil), rep.InstalledAgents...)
	sort.Strings(agts)
	if len(agts) > 0 {
		fmt.Fprintln(&b, app.th.faint.Render("agents: "+strings.Join(agts, ", ")))
	} else {
		fmt.Fprintln(&b, app.th.faint.Render("no agents — skills are in .agents/skills/"))
	}
	if len(rep.FailedAgents) > 0 {
		fmt.Fprintln(&b, app.th.errorText.Render("failed: "+strings.Join(rep.FailedAgents, ", ")))
	}
	if rep.InstalledInstruction != "" {
		fmt.Fprintln(&b, app.th.faint.Render("updated "+rep.InstalledInstruction))
	}

	if rep.Audit != nil && len(rep.Audit.Findings) > 0 {
		fmt.Fprintf(&b, "\n%s\n", app.th.errorText.Render(fmt.Sprintf("⚠ %d security finding(s):", len(rep.Audit.Findings))))
		for _, f := range rep.Audit.Findings {
			fmt.Fprintf(&b, "  • [%s] %s %s\n", f.Severity, f.Kind, f.Skill)
		}
	}

	fmt.Fprintf(&b, "\n%s", app.helpView(app.keys.Back, app.keys.Confirm))
	return b.String()
}
