package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/security/sanitize"
	"github.com/louisescher/hangar/internal/spec"
)

// Options configures a TUI run.
type Options struct {
	Engine     EngineAPI
	Prefs      config.Prefs
	Global     bool
	Security   sanitize.Opts
	Agents     []string           // explicit agent names from -a (optional)
	StartSpec  *spec.SourceSpec   // set when launched via `hangar install <spec>`; skips the catalog
	Discovered *engine.Discovered // pre-fetched source; skips the crawl. Closed by Run.
	Manage     bool               // start on the manage-installed screen (`hangar list`)
	Info       bool               // start on the info browser (`hangar info`)
	Profiles   bool               // start on the profile browser (`hangar profile list`)
	Ctx        context.Context    // base context (cancelled on SIGINT)
}

// screen is one step of the flow. Screens read and mutate shared session state
// on *App but never set App.state directly; they emit a navMsg instead.
type screen interface {
	enter(app *App) tea.Cmd
	update(app *App, msg tea.Msg) tea.Cmd
	view(app *App) string
	capturing() bool // true when a text input is focused (suppress global hotkeys)
}

// App is the root bubbletea model.
type App struct {
	eng     EngineAPI
	keys    keyMap
	th      theme
	prefs   config.Prefs
	baseCtx context.Context

	width, height int
	state         state
	history       []state // back-stack of visited screens (most recent last)
	err           error
	quitting      bool

	// run configuration
	global   bool
	security sanitize.Opts
	agents   []string

	// async control for the in-flight operation
	ctx    context.Context
	cancel context.CancelFunc

	// session state, threaded across screens
	src         spec.SourceSpec
	discovered  *engine.Discovered
	selection   *skillSet
	chosen      []engine.Agent
	report      *install.Report
	infoSkill   *engine.Skill // the skill shown on the info detail screen
	profileName string        // the profile open on the profile detail screen

	// screens
	catalog       catalogModel
	crawling      crawlingModel
	tree          treeModel
	agentsScr     agentsModel
	optionsScr    optionsModel
	confirmScr    confirmModel
	installing    installingModel
	results       resultsModel
	manage        manageModel
	infoList      infoListModel
	infoDetail    infoDetailModel
	profileList   profileListModel
	profileDetail profileDetailModel
}

// reresolveAgents re-resolves the chosen agents against the current scope
// (called when the scope toggles after agents were picked).
func (m *App) reresolveAgents() {
	if len(m.chosen) == 0 {
		return
	}
	names := make([]string, 0, len(m.chosen))
	for _, a := range m.chosen {
		names = append(names, a.Def.Name)
	}
	if agts, err := m.eng.ResolveAgents(names, m.global); err == nil {
		m.chosen = agts
	}
}

// saveScopeSanitize persists the current scope and sanitize options to prefs.
func (m *App) saveScopeSanitize() {
	if m.global {
		m.prefs.Scope = config.ScopeGlobal
	} else {
		m.prefs.Scope = config.ScopeLocal
	}
	m.prefs.StripInvisible = m.security.StripInvisible
	m.prefs.StripComments = m.security.StripComments
	_ = m.prefs.Save()
}

// selectedSkills returns the discovered skills the user has checked.
func (m *App) selectedSkills() []engine.Skill {
	var out []engine.Skill
	if m.discovered == nil {
		return out
	}
	for i := range m.discovered.Skills {
		if m.selection.has(skillKey(&m.discovered.Skills[i])) {
			out = append(out, m.discovered.Skills[i])
		}
	}
	return out
}

// New constructs the root model.
func New(opt Options) *App {
	base := opt.Ctx
	if base == nil {
		base = context.Background()
	}
	if opt.Security == (sanitize.Opts{}) {
		opt.Security = sanitize.InvisibleOnly
	}
	m := &App{
		eng:       opt.Engine,
		keys:      defaultKeys(),
		th:        newTheme(),
		prefs:     opt.Prefs,
		baseCtx:   base,
		global:    opt.Global,
		security:  opt.Security,
		agents:    opt.Agents,
		selection: newSkillSet(),
	}
	switch {
	case opt.Info:
		m.discovered = opt.Discovered
		if opt.Discovered != nil && len(opt.Discovered.Skills) == 1 {
			m.infoSkill = &opt.Discovered.Skills[0]
			m.state = stateInfoDetail
		} else {
			m.state = stateInfoList
		}
	case opt.Profiles:
		m.state = stateProfileList
	case opt.Manage:
		m.state = stateManage
	case opt.Discovered != nil:
		// Pre-fetched: skip catalog and crawl, go straight to picking.
		if opt.StartSpec != nil {
			m.src = *opt.StartSpec
		}
		m.discovered = opt.Discovered
		if len(opt.Discovered.Skills) == 0 {
			m.state = stateAgents // references-only source
		} else {
			m.state = stateTree
		}
	case opt.StartSpec != nil:
		m.src = *opt.StartSpec
		m.state = stateCrawling
	default:
		m.state = stateCatalog
	}
	return m
}

func (m *App) Init() tea.Cmd {
	return m.activeScreen().enter(m)
}

// startOp creates a fresh cancellable context for an async operation.
func (m *App) startOp() context.Context {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = context.WithCancel(m.baseCtx)
	return m.ctx
}

// quit cancels any in-flight op and ends the program.
func (m *App) quit() tea.Cmd {
	m.quitting = true
	if m.cancel != nil {
		m.cancel()
	}
	return tea.Quit
}

// transition switches to another screen and runs its enter hook.
func (m *App) transition(to state) tea.Cmd {
	m.state = to
	m.err = nil
	return m.activeScreen().enter(m)
}

// back returns to the previous screen on the back-stack, or quits when the stack
// is empty (i.e. we're on the flow's entry screen).
func (m *App) back() tea.Cmd {
	if len(m.history) == 0 {
		return m.quit()
	}
	prev := m.history[len(m.history)-1]
	m.history = m.history[:len(m.history)-1]
	return m.transition(prev)
}

func (m *App) activeScreen() screen {
	switch m.state {
	case stateCatalog:
		return &m.catalog
	case stateCrawling:
		return &m.crawling
	case stateTree:
		return &m.tree
	case stateAgents:
		return &m.agentsScr
	case stateOptions:
		return &m.optionsScr
	case stateConfirm:
		return &m.confirmScr
	case stateInstalling:
		return &m.installing
	case stateResults:
		return &m.results
	case stateManage:
		return &m.manage
	case stateInfoList:
		return &m.infoList
	case stateInfoDetail:
		return &m.infoDetail
	case stateProfileList:
		return &m.profileList
	case stateProfileDetail:
		return &m.profileDetail
	default:
		return &m.catalog
	}
}

func (m *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// fall through: let the active screen size its sub-components too.
		cmd := m.activeScreen().update(m, msg)
		return m, cmd

	case tea.KeyMsg:
		// ctrl+c always quits, even while typing in a filter.
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		sc := m.activeScreen()
		if !sc.capturing() {
			if key.Matches(msg, m.keys.Quit) {
				return m, m.quit()
			}
		}
		return m, sc.update(m, msg)

	case navMsg:
		m.history = append(m.history, m.state)
		return m, m.transition(msg.to)

	case navBackMsg:
		return m, m.back()

	case navReplaceMsg:
		return m, m.transition(msg.to)

	case errMsg:
		m.err = msg.err
		// Let the active screen react (e.g. stop a spinner) too.
		return m, m.activeScreen().update(m, msg)

	default:
		return m, m.activeScreen().update(m, msg)
	}
}

func (m *App) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "" // wait for the first WindowSizeMsg
	}

	header := m.headerView()
	footer := m.footerView()
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	body := m.activeScreen().view(m)
	body = lipgloss.NewStyle().Height(bodyHeight).MaxHeight(bodyHeight).Render(body)

	return strings.Join([]string{header, body, footer}, "\n")
}

func (m *App) headerView() string {
	title := m.th.title.Render("✈ hangar")
	ctx := m.contextLabel()
	if ctx != "" {
		title += "  " + m.th.subtitle.Render(ctx)
	}
	return title
}

// contextLabel describes the current source/scope for the header.
func (m *App) contextLabel() string {
	var parts []string
	if m.discovered != nil {
		s := m.discovered.Source
		if m.discovered.Ref != "" {
			s += "@" + m.discovered.Ref
		}
		parts = append(parts, s)
	} else if m.src.Raw != "" {
		parts = append(parts, m.src.Raw)
	}
	scope := "local"
	if m.global {
		scope = "global"
	}
	parts = append(parts, scope)
	return strings.Join(parts, " · ")
}

func (m *App) footerView() string {
	if m.err != nil {
		return m.th.errorText.Render("error: " + m.err.Error())
	}
	return ""
}

// bodyHeight returns the height available to a screen's body.
func (m *App) bodyHeight() int {
	h := m.height - lipgloss.Height(m.headerView()) - lipgloss.Height(m.footerView())
	if h < 1 {
		return 1
	}
	return h
}

// helpView renders a one-line help footer from key bindings.
func (m *App) helpView(bs ...key.Binding) string {
	var parts []string
	for _, b := range bs {
		h := b.Help()
		parts = append(parts, h.Key+" "+h.Desc)
	}
	return m.th.help.Render(strings.Join(parts, " · "))
}
