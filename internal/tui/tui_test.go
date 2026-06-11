package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/index"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/spec"
)

// fakeEngine implements EngineAPI with canned data for driving the TUI in tests.
type fakeEngine struct {
	disc         *engine.Discovered
	installCalls int
	lastSkills   []engine.Skill
	checkCalls   int
	removed      map[string]bool
}

func (f *fakeEngine) Index() []index.Entry {
	return []index.Entry{{Name: "demo", Spec: "owner/repo", Description: "a demo source"}}
}
func (f *fakeEngine) Discover(_ context.Context, _ spec.SourceSpec) (*engine.Discovered, error) {
	return f.disc, nil
}
func (f *fakeEngine) ReadSkillDoc(sk engine.Skill) (engine.SkillDoc, error) {
	return engine.SkillDoc{Skill: sk, Body: "# " + sk.Name + "\n\nbody"}, nil
}
func (f *fakeEngine) DetectAgents(bool) ([]agents.Agent, error) {
	return []agents.Agent{{Def: agents.Def{Name: "claude", Display: "Claude Code"}, InstallPath: ".claude/skills", Detected: true}}, nil
}
func (f *fakeEngine) ResolveAgents(names []string, _ bool) ([]agents.Agent, error) {
	var out []agents.Agent
	for _, n := range names {
		out = append(out, agents.Agent{Def: agents.Def{Name: n, Display: n}, InstallPath: "." + n + "/skills"})
	}
	return out, nil
}
func (f *fakeEngine) AgentDefs() []agents.Def {
	return []agents.Def{
		{Name: "claude", Display: "Claude Code", DetectDir: ".claude"},
		{Name: "cursor", Display: "Cursor", DetectDir: ".cursor"},
	}
}
func (f *fakeEngine) Installed(bool) ([]lockfile.Entry, error) {
	all := []lockfile.Entry{
		{Name: "pdf", Source: "owner/repo", Ref: "main", Kind: lockfile.KindSkill},
		{Name: "readme", Source: "npm:demo", Version: "1.0.0", Kind: lockfile.KindRef},
	}
	var out []lockfile.Entry
	for _, e := range all {
		if !f.removed[e.Name] {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f *fakeEngine) CheckUpdates(_ context.Context, _ bool) ([]engine.InstalledStatus, error) {
	f.checkCalls++
	ents, _ := f.Installed(false)
	var out []engine.InstalledStatus
	for _, e := range ents {
		st := engine.InstalledStatus{Entry: e, Latest: entryRefLabel(e)}
		if e.Name == "pdf" {
			st.Outdated = true
		}
		out = append(out, st)
	}
	return out, nil
}
func (f *fakeEngine) Update(_ context.Context, _ string, _ engine.InstallOptions) (install.Report, error) {
	return install.Report{Skills: []install.SkillResult{{Name: "pdf", Kind: lockfile.KindSkill}}}, nil
}
func (f *fakeEngine) UpdateNames(_ context.Context, names []string, _ engine.InstallOptions) (install.Report, error) {
	var res []install.SkillResult
	for _, n := range names {
		res = append(res, install.SkillResult{Name: n, Kind: lockfile.KindSkill})
	}
	return install.Report{Skills: res}, nil
}
func (f *fakeEngine) Remove(name string, _ engine.InstallOptions) error {
	if f.removed == nil {
		f.removed = map[string]bool{}
	}
	f.removed[name] = true
	return nil
}
func (f *fakeEngine) PreviewUpdate(_ context.Context, name string, _ engine.InstallOptions) (string, error) {
	return "--- " + name + "\n+++ " + name + "\n@@ -1 +1 @@\n-old\n+new\n", nil
}
func (f *fakeEngine) SetPinned(string, bool, engine.InstallOptions) error { return nil }

func (f *fakeEngine) InstallSelected(_ *engine.Discovered, _ spec.SourceSpec, skills []engine.Skill, _ []discover.RefDoc, agts []agents.Agent, _ engine.InstallOptions) (install.Report, error) {
	f.installCalls++
	f.lastSkills = skills
	var res []install.SkillResult
	for _, sk := range skills {
		res = append(res, install.SkillResult{Name: sk.Name, Kind: lockfile.KindSkill})
	}
	var names []string
	for _, a := range agts {
		names = append(names, a.Def.Name)
	}
	return install.Report{Skills: res, InstalledAgents: names}, nil
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// pump feeds the result of a transition command back into the model until it
// settles, without executing batches (which schedule spinner ticks).
func pump(m *App, cmd tea.Cmd) {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		if _, ok := msg.(tea.BatchMsg); ok {
			return
		}
		_, cmd = m.Update(msg)
	}
}

func sampleDiscovered() *engine.Discovered {
	return &engine.Discovered{
		Source: "owner/repo",
		Ref:    "v1.0.0",
		Skills: []engine.Skill{
			{Name: "pdf", Description: "PDFs", RelPath: "skills/pdf", AbsPath: "/x/skills/pdf"},
			{Name: "docx", Description: "Word", RelPath: "skills/docx", AbsPath: "/x/skills/docx"},
			{Name: "scrape", Description: "Web", RelPath: "web/scrape", AbsPath: "/x/web/scrape"},
		},
	}
}

func TestFlowEndToEnd(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate prefs writes

	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.Init() != nil {
		// catalog enter has no cmd; fine either way.
	}

	if m.state != stateCatalog {
		t.Fatalf("start state = %v, want catalog", m.state)
	}
	if !strings.Contains(m.View(), "demo") {
		t.Errorf("catalog view should list the demo entry:\n%s", m.View())
	}

	// Choose the highlighted catalog entry → crawl.
	_, cmd := m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateCrawling {
		t.Fatalf("after choose, state = %v, want crawling", m.state)
	}

	// Simulate the crawl completing.
	_, cmd = m.Update(discoveredMsg{d: fake.disc})
	pump(m, cmd)
	if m.state != stateTree {
		t.Fatalf("after discover, state = %v, want tree", m.state)
	}

	// Tree seeds all skills selected.
	if got := m.selection.count(); got != 3 {
		t.Fatalf("default selection = %d, want 3", got)
	}

	// Cursor starts on the "skills" folder; toggling it clears its two skills.
	m.Update(keyMsg("space"))
	if got := m.selection.count(); got != 1 {
		t.Errorf("after folder toggle, selection = %d, want 1", got)
	}

	// Toggle to list view; the preference is persisted.
	m.Update(keyMsg("t"))
	if !m.tree.listView {
		t.Error("expected list view after 't'")
	}
	if p, _ := config.LoadPrefs(); p.View != config.ViewList {
		t.Errorf("view pref = %q, want list", p.View)
	}

	// Filter to just pdf.
	m.Update(keyMsg("/"))
	m.Update(keyMsg("pdf"))
	if len(m.tree.rows) != 1 {
		t.Errorf("filtered rows = %d, want 1", len(m.tree.rows))
	}
	m.Update(keyMsg("esc")) // clear filter

	// Select everything, then continue to agents.
	m.Update(keyMsg("a"))
	if got := m.selection.count(); got != 3 {
		t.Errorf("after select-all, selection = %d, want 3", got)
	}
	_, cmd = m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateAgents {
		t.Fatalf("state = %v, want agents", m.state)
	}

	// Claude is detected → pre-selected. Continue to the options screen.
	_, cmd = m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateOptions {
		t.Fatalf("state = %v, want options", m.state)
	}
	if len(m.chosen) != 1 || m.chosen[0].Def.Name != "claude" {
		t.Errorf("chosen agents = %+v, want [claude]", m.chosen)
	}

	// Options → review.
	_, cmd = m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateConfirm {
		t.Fatalf("state = %v, want confirm", m.state)
	}

	// Review → installing.
	_, cmd = m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateInstalling {
		t.Fatalf("state = %v, want installing", m.state)
	}

	// Drive the install command (exercises InstallSelected wiring), then feed
	// its completion back into the model.
	ch := make(chan install.Event, 8)
	done := installRun(fake, m.discovered, m.src, m.selectedSkills(), nil, m.chosen, engine.InstallOptions{}, ch)()
	_, cmd = m.Update(done)
	pump(m, cmd)

	if fake.installCalls != 1 {
		t.Errorf("InstallSelected called %d times, want 1", fake.installCalls)
	}
	if len(fake.lastSkills) != 3 {
		t.Errorf("installed %d skills, want 3", len(fake.lastSkills))
	}
	if m.state != stateResults {
		t.Fatalf("state = %v, want results", m.state)
	}
	if m.report == nil || len(m.report.Skills) != 3 {
		t.Errorf("report missing or wrong: %+v", m.report)
	}
	if !strings.Contains(m.View(), "done") {
		t.Errorf("results view should say done:\n%s", m.View())
	}
}

func TestManageScreen(t *testing.T) {
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Manage: true, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init() // batched (spinner + check); driven explicitly below
	if m.state != stateManage {
		t.Fatalf("start state = %v, want manage", m.state)
	}
	if len(m.manage.rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(m.manage.rows))
	}

	// The async update-check attaches statuses (pdf is outdated).
	status := manageCheckCmd(context.Background(), fake, false)().(manageStatusMsg)
	m.Update(status)
	if st := m.manage.rows[0].status; st == nil || !st.Outdated {
		t.Errorf("pdf row should be flagged outdated, got %+v", st)
	}

	// 'x' asks to confirm removal; the screen captures keys while confirming.
	m.Update(keyMsg("x"))
	if m.manage.confirmRemove != "pdf" {
		t.Errorf("confirmRemove = %q, want pdf", m.manage.confirmRemove)
	}
	if !m.manage.capturing() {
		t.Error("manage should capture input while confirming a removal")
	}
	// 'n' cancels.
	m.Update(keyMsg("n"))
	if m.manage.confirmRemove != "" {
		t.Error("expected 'n' to cancel the removal confirmation")
	}

	// 'enter' previews the pending diff (async); feeding the result shows it.
	m.Update(keyMsg("enter"))
	if !m.manage.busy {
		t.Error("expected busy while computing the diff")
	}
	d, _ := fake.PreviewUpdate(context.Background(), "pdf", engine.InstallOptions{})
	m.Update(manageDiffMsg{name: "pdf", diff: d})
	if !m.manage.previewing {
		t.Error("expected the diff preview to open")
	}
	if !m.manage.capturing() {
		t.Error("the diff view should capture input")
	}
	m.Update(keyMsg("esc")) // close the diff
	if m.manage.previewing {
		t.Error("esc should close the diff view")
	}

	// 'p' toggles the pin on the highlighted entry.
	m.Update(keyMsg("p"))
	if m.manage.note == "" {
		t.Error("pin toggle should set a status note")
	}

	// 'u' kicks off an update (sets busy; the work runs in a batched command).
	m.Update(keyMsg("u"))
	if !m.manage.busy {
		t.Error("expected busy after pressing u")
	}

	// The update command itself drives Update through the engine.
	op, ok := manageUpdateCmd(context.Background(), fake, "pdf", false)().(manageOpMsg)
	if !ok || op.err != nil {
		t.Fatalf("manageUpdateCmd produced %#v", op)
	}

	// Feeding completion back clears busy and keeps us on the manage screen.
	m.Update(op)
	if m.manage.busy {
		t.Error("expected not busy after the op completes")
	}
	if m.state != stateManage {
		t.Fatalf("state drifted to %v during manage ops", m.state)
	}
}

func bigDiscovered(n int) *engine.Discovered {
	d := &engine.Discovered{Source: "owner/repo", Ref: "main"}
	for i := 0; i < n; i++ {
		name := "skill" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		d.Skills = append(d.Skills, engine.Skill{Name: name, RelPath: "skills/" + name, AbsPath: "/x/" + name})
	}
	return d
}

func TestTreeCollapseScrollAndPreviewFocus(t *testing.T) {
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Discovered: fake.disc, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	// Tree rows: skills(folder), docx, pdf, web(folder), scrape.
	before := len(m.tree.rows)
	if before != 5 {
		t.Fatalf("expected 5 rows, got %d", before)
	}
	// Cursor on the "skills" folder; collapsing it must hide its children.
	m.Update(keyMsg("left"))
	if len(m.tree.rows) >= before {
		t.Errorf("collapse should reduce visible rows: before=%d after=%d", before, len(m.tree.rows))
	}
	// Expanding restores them.
	m.Update(keyMsg("right"))
	if len(m.tree.rows) != before {
		t.Errorf("expand should restore rows: want %d got %d", before, len(m.tree.rows))
	}

	// Preview focus: tab on a skill focuses the preview; esc returns.
	m.Update(keyMsg("down")) // move onto a skill leaf
	m.Update(keyMsg("tab"))
	if !m.tree.previewFocused {
		t.Error("tab on a skill should focus the preview")
	}
	m.Update(keyMsg("esc"))
	if m.tree.previewFocused {
		t.Error("esc should return focus to the list")
	}

	// Scrolling: a tall tree in a short window must advance the offset.
	big := &fakeEngine{disc: bigDiscovered(30)}
	m2 := New(Options{Engine: big, Prefs: config.DefaultPrefs(), Discovered: big.disc, Ctx: context.Background()})
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m2.Init()
	for i := 0; i < 20; i++ {
		m2.Update(keyMsg("down"))
	}
	if m2.tree.offset == 0 {
		t.Error("moving the cursor below the fold should scroll the tree (offset stayed 0)")
	}
}

func TestInfoListToDetail(t *testing.T) {
	fake := &fakeEngine{disc: sampleDiscovered()} // 3 skills
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Info: true, Discovered: fake.disc, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	if m.state != stateInfoList {
		t.Fatalf("multi-skill source should open the info list, got %v", m.state)
	}
	if len(m.infoList.rows) != 3 {
		t.Fatalf("info list rows = %d, want 3", len(m.infoList.rows))
	}

	// Select the first skill → detail.
	_, cmd := m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateInfoDetail {
		t.Fatalf("state = %v, want info detail", m.state)
	}
	if m.infoSkill == nil {
		t.Fatal("infoSkill should be set after selecting")
	}
	// The detail view renders metadata (the skill name) for the selection.
	if !strings.Contains(m.View(), m.infoSkill.Name) {
		t.Errorf("detail view should show the skill name %q", m.infoSkill.Name)
	}

	// tab focuses the doc pane; esc returns to the list (multi-skill source).
	m.Update(keyMsg("tab"))
	if !m.infoDetail.focused {
		t.Error("tab should focus the doc pane")
	}
	m.Update(keyMsg("esc"))
	if m.infoDetail.focused {
		t.Error("esc should unfocus the doc pane")
	}
	_, cmd = m.Update(keyMsg("esc"))
	pump(m, cmd)
	if m.state != stateInfoList {
		t.Errorf("esc on detail (multi-skill) should return to the list, got %v", m.state)
	}
}

func TestInfoSingleSkillOpensDetail(t *testing.T) {
	one := &engine.Discovered{Source: "owner/repo", Ref: "v1", Skills: []engine.Skill{
		{Name: "solo", Description: "the only one", RelPath: "skills/solo", AbsPath: "/x/solo"},
	}}
	fake := &fakeEngine{disc: one}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Info: true, Discovered: one, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()
	if m.state != stateInfoDetail {
		t.Fatalf("single-skill source should open the detail directly, got %v", m.state)
	}
	if m.infoSkill == nil || m.infoSkill.Name != "solo" {
		t.Errorf("infoSkill = %+v, want solo", m.infoSkill)
	}
}

func TestManageShowsDeletedUpstream(t *testing.T) {
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Manage: true, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	// A source that was deleted upstream surfaces as Gone, not outdated/removed.
	m.Update(manageStatusMsg{statuses: []engine.InstalledStatus{
		{Entry: lockfile.Entry{Name: "pdf", Source: "owner/repo", Ref: "main", Kind: lockfile.KindSkill}, Gone: true},
		{Entry: lockfile.Entry{Name: "readme", Source: "npm:demo", Version: "1.0.0", Kind: lockfile.KindRef}},
	}})
	if st := m.manage.rows[0].status; st == nil || !st.Gone {
		t.Fatalf("pdf row should be flagged gone, got %+v", st)
	}
	if !strings.Contains(m.View(), "deleted") {
		t.Errorf("manage view should show a 'deleted' status for a gone skill:\n%s", m.View())
	}
	// The skill is kept (still listed) for manual removal.
	if len(m.manage.rows) != 2 {
		t.Errorf("a gone skill must stay listed for manual removal, rows = %d", len(m.manage.rows))
	}
}

func TestManageRemoveKeepsCachedStatus(t *testing.T) {
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Manage: true, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	// Initial update-check populates the cache for both rows.
	status := manageCheckCmd(context.Background(), fake, false)().(manageStatusMsg)
	m.Update(status)
	if fake.checkCalls != 1 {
		t.Fatalf("checkCalls = %d, want 1", fake.checkCalls)
	}
	if len(m.manage.rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(m.manage.rows))
	}

	// Remove pdf. The op must NOT trigger another update-check, and readme's
	// cached "up to date" status must survive.
	op := manageRemoveCmd(fake, "pdf", false)().(manageOpMsg)
	m.Update(op)

	if fake.checkCalls != 1 {
		t.Errorf("remove triggered a re-check: checkCalls = %d, want 1", fake.checkCalls)
	}
	if len(m.manage.rows) != 1 || m.manage.rows[0].entry.Name != "readme" {
		t.Fatalf("rows after remove = %+v, want [readme]", m.manage.rows)
	}
	if st := m.manage.rows[0].status; st == nil {
		t.Error("readme should keep its cached status after removing pdf")
	}
}

func TestOptionsToggleScopeAndSanitize(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Discovered: fake.disc, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()
	m.chosen = []engine.Agent{{Def: agents.Def{Name: "claude"}, InstallPath: ".claude/skills"}}

	// Jump straight to the options screen and toggle scope (cursor on row 0).
	m.state = stateOptions
	wasGlobal := m.global
	m.Update(keyMsg("space"))
	if m.global == wasGlobal {
		t.Error("space on the scope row should toggle global")
	}
	// Chosen agents re-resolve for the new scope (path changes for global).
	if len(m.chosen) != 1 {
		t.Errorf("chosen agents lost on scope toggle: %+v", m.chosen)
	}

	// Move to strip-invisible and toggle it off.
	before := m.security.StripInvisible
	m.Update(keyMsg("down"))
	m.Update(keyMsg("space"))
	if m.security.StripInvisible == before {
		t.Error("space on the strip-invisible row should toggle it")
	}
	// Enter advances to review.
	_, cmd := m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateConfirm {
		t.Fatalf("enter on options should advance to review, got state %v", m.state)
	}
}

func TestSelectionSurvivesAgentsRoundTrip(t *testing.T) {
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Discovered: fake.disc, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	// Deselect the "skills" folder (pdf+docx) → 1 remaining.
	m.Update(keyMsg("space"))
	want := m.selection.count()
	if want != 1 {
		t.Fatalf("setup: selection = %d, want 1", want)
	}

	// Continue to agents, then go back: the selection must be preserved.
	_, cmd := m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateAgents {
		t.Fatalf("state = %v, want agents", m.state)
	}
	_, cmd = m.Update(keyMsg("esc"))
	pump(m, cmd)
	if m.state != stateTree {
		t.Fatalf("esc from agents should return to the tree, got %v", m.state)
	}
	if got := m.selection.count(); got != want {
		t.Errorf("selection dropped on round-trip: want %d, got %d", want, got)
	}
}

func TestTreeBlocksEmptySelection(t *testing.T) {
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Discovered: fake.disc, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	// Clear the selection, then try to continue.
	m.Update(keyMsg("n"))
	if m.selection.count() != 0 {
		t.Fatalf("expected empty selection after 'n', got %d", m.selection.count())
	}
	_, cmd := m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateTree {
		t.Errorf("continuing with 0 skills must stay on the tree, got %v", m.state)
	}
	if m.tree.notice == "" {
		t.Error("expected a validation notice when continuing with 0 skills")
	}

	// Selecting something clears the notice and lets the flow proceed.
	m.Update(keyMsg("a"))
	_, cmd = m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateAgents {
		t.Errorf("after selecting, enter should advance to agents, got %v", m.state)
	}
}

func TestReviewShortcutBackReturnsToReview(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Discovered: fake.disc, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	// Walk to the review screen: tree → agents → options → review.
	for i := 0; i < 3; i++ {
		_, cmd := m.Update(keyMsg("enter"))
		pump(m, cmd)
	}
	if m.state != stateConfirm {
		t.Fatalf("expected the review screen, got %v", m.state)
	}

	// From review, jump to options via the 'o' shortcut, then press esc: back
	// must return to review (the screen we came from), not the linear predecessor.
	_, cmd := m.Update(keyMsg("o"))
	pump(m, cmd)
	if m.state != stateOptions {
		t.Fatalf("'o' on review should open options, got %v", m.state)
	}
	_, cmd = m.Update(keyMsg("esc"))
	pump(m, cmd)
	if m.state != stateConfirm {
		t.Errorf("esc from options (reached via review) should return to review, got %v", m.state)
	}

	// A plain back from review follows the linear path to options.
	_, cmd = m.Update(keyMsg("esc"))
	pump(m, cmd)
	if m.state != stateOptions {
		t.Errorf("esc from review should go to options, got %v", m.state)
	}
}

func TestProfileBrowserInspectAndRemove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.SaveProfile(config.Profile{Name: "dev", Skills: []lockfile.Entry{
		{Name: "pdf", Source: "owner/repo", Ref: "main", Kind: lockfile.KindSkill},
		{Name: "docx", Source: "owner/repo", Ref: "main", Kind: lockfile.KindSkill},
	}}); err != nil {
		t.Fatal(err)
	}

	fake := &fakeEngine{}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Profiles: true, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()
	if m.state != stateProfileList {
		t.Fatalf("start state = %v, want profile list", m.state)
	}
	if len(m.profileList.names) != 1 || m.profileList.names[0] != "dev" {
		t.Fatalf("profile names = %v, want [dev]", m.profileList.names)
	}

	// Select the profile → detail with its two entries.
	_, cmd := m.Update(keyMsg("enter"))
	pump(m, cmd)
	if m.state != stateProfileDetail {
		t.Fatalf("state = %v, want profile detail", m.state)
	}
	if len(m.profileDetail.entries) != 2 {
		t.Fatalf("detail entries = %d, want 2", len(m.profileDetail.entries))
	}

	// Remove the highlighted entry (x then y) — it must persist to disk.
	m.Update(keyMsg("x"))
	if m.profileDetail.confirmRemove == "" {
		t.Fatal("expected a remove confirmation")
	}
	m.Update(keyMsg("y"))
	if len(m.profileDetail.entries) != 1 {
		t.Fatalf("after remove, entries = %d, want 1", len(m.profileDetail.entries))
	}
	if p, _ := config.LoadProfile("dev"); len(p.Skills) != 1 {
		t.Errorf("on-disk profile should have 1 entry, got %d", len(p.Skills))
	}

	// esc returns to the profile list.
	_, cmd = m.Update(keyMsg("esc"))
	pump(m, cmd)
	if m.state != stateProfileList {
		t.Errorf("esc on detail should return to the profile list, got %v", m.state)
	}
}

func TestTreeFilterAndViewToggleKeepSelection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fake := &fakeEngine{disc: sampleDiscovered()}
	m := New(Options{Engine: fake, Prefs: config.DefaultPrefs(), Discovered: fake.disc, Ctx: context.Background()})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()

	if m.state != stateTree {
		t.Fatalf("pre-seeded start = %v, want tree", m.state)
	}
	// Deselect one skill, then toggle the view: selection must survive.
	m.Update(keyMsg("space")) // toggles the "skills" folder off → count 1
	before := m.selection.count()
	m.Update(keyMsg("t"))
	if m.selection.count() != before {
		t.Errorf("view toggle changed selection: %d -> %d", before, m.selection.count())
	}
}
