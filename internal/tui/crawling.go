package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/louisescher/hangar/internal/spec"
)

// crawlingModel shows a spinner while the source is fetched and crawled.
type crawlingModel struct {
	sp    spinner.Model
	done  bool
	ready bool
}

func (s *crawlingModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.sp = spinner.New()
		s.sp.Spinner = spinner.Dot
		s.ready = true
	}
	s.done = false
	ctx := app.startOp()
	return tea.Batch(s.sp.Tick, discoverCmd(ctx, app.eng, app.src))
}

func (s *crawlingModel) capturing() bool { return false }

func (s *crawlingModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			return func() tea.Msg { return navBackMsg{} }
		}
		// Re-arm the spinner so input never stalls the animation. A duplicate
		// tick self-prunes via the spinner's tag check.
		return s.sp.Tick
	case discoveredMsg:
		app.discovered = msg.d
		// The tree screen seeds defaults. Replace (not push) so esc from the tree
		// goes back to the catalog, not to this finished crawl.
		return func() tea.Msg { return navReplace(stateTree) }
	case errMsg:
		s.done = true
		return nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		s.sp, cmd = s.sp.Update(msg)
		return cmd
	}
	return nil
}

func (s *crawlingModel) view(app *App) string {
	var b strings.Builder
	b.WriteString("\n")
	if app.err != nil {
		fmt.Fprintf(&b, "%s\n\n", app.th.errorText.Render("could not fetch "+app.src.Raw))
		fmt.Fprintf(&b, "%s\n\n", app.th.faint.Render("  "+app.err.Error()))
		b.WriteString(app.helpView(app.keys.Back, app.keys.Quit))
		return b.String()
	}
	fmt.Fprintf(&b, "  %s fetching %s …\n\n", s.sp.View(), app.th.accent.Render(sourceText(app.src)))
	b.WriteString(app.helpView(app.keys.Back))
	return b.String()
}

func sourceText(s spec.SourceSpec) string {
	if s.Raw != "" {
		return s.Raw
	}
	switch s.Kind {
	case spec.KindNPM:
		return "npm:" + s.Pkg
	case spec.KindGitHub:
		return s.Owner + "/" + s.Repo
	default:
		return s.Path
	}
}

// discoverCmd runs the (blocking) crawl off the UI goroutine.
func discoverCmd(ctx context.Context, eng EngineAPI, s spec.SourceSpec) tea.Cmd {
	return func() tea.Msg {
		d, err := eng.Discover(ctx, s)
		if err != nil {
			return errMsg{err}
		}
		return discoveredMsg{d}
	}
}
