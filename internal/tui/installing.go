package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/spec"
)

// installingModel runs the install off the UI goroutine, streaming progress
// events through a channel into a progress bar.
type installingModel struct {
	sp          spinner.Model
	prog        progress.Model
	ch          chan install.Event
	done, total int
	step        string
	ready       bool
}

func (s *installingModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.sp = spinner.New()
		s.sp.Spinner = spinner.Dot
		s.prog = progress.New(progress.WithDefaultGradient())
		s.ready = true
	}
	s.done, s.total, s.step = 0, 0, ""
	s.ch = make(chan install.Event, 64)
	ch := s.ch

	opt := engine.InstallOptions{
		Global:   app.global,
		Security: app.security,
		OnProgress: func(ev install.Event) {
			select {
			case ch <- ev: // best-effort; never block the install on the UI
			default:
			}
		},
	}
	var refs []discover.RefDoc
	if app.discovered != nil {
		refs = app.discovered.References
	}
	return tea.Batch(
		s.sp.Tick,
		installProgressWait(ch),
		installRun(app.eng, app.discovered, app.src, app.selectedSkills(), refs, app.chosen, opt, ch),
	)
}

func (s *installingModel) capturing() bool { return false }

func (s *installingModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case progressMsg:
		if !msg.ok {
			return nil // channel closed; install goroutine will report done
		}
		s.done, s.total = msg.ev.Index, msg.ev.Total
		s.step = strings.TrimSpace(msg.ev.Phase + " " + msg.ev.Name)
		return installProgressWait(s.ch)
	case installDoneMsg:
		if msg.err != nil {
			app.err = msg.err
		} else {
			r := msg.report
			app.report = &r
		}
		return func() tea.Msg { return navReplace(stateResults) }
	case spinner.TickMsg:
		var cmd tea.Cmd
		s.sp, cmd = s.sp.Update(msg)
		return cmd
	case tea.KeyMsg:
		// Re-arm the spinner so input never stalls the animation.
		return s.sp.Tick
	}
	return nil
}

func (s *installingModel) view(app *App) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s installing…\n\n", s.sp.View())

	if s.total > 0 {
		s.prog.Width = clampWidth(app.width-6, 20, 60)
		pct := float64(s.done) / float64(s.total)
		fmt.Fprintf(&b, "  %s\n\n", s.prog.ViewAs(pct))
	}
	if s.step != "" {
		fmt.Fprintf(&b, "  %s\n", app.th.faint.Render(s.step))
	}
	return b.String()
}

func clampWidth(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// installProgressWait reads one progress event from the bridge channel.
func installProgressWait(ch chan install.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return progressMsg{ev: ev, ok: ok}
	}
}

// installRun performs the install and closes the progress channel when done so
// the progress reader stops.
func installRun(eng EngineAPI, d *engine.Discovered, src spec.SourceSpec, skills []engine.Skill, refs []discover.RefDoc, agts []engine.Agent, opt engine.InstallOptions, ch chan install.Event) tea.Cmd {
	return func() tea.Msg {
		rep, err := eng.InstallSelected(d, src, skills, refs, agts, opt)
		close(ch)
		return installDoneMsg{report: rep, err: err}
	}
}
