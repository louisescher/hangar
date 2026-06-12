package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/spec"
)

type checkState int

const (
	unchecked checkState = iota
	partial
	checked
)

// node is one element of the skill tree: a folder (intermediate) or a skill
// leaf. Only paths that lead to a skill are present (the tree is pruned).
type node struct {
	label    string
	key      string // selection key (skill leaves only)
	isSkill  bool
	skill    *engine.Skill
	children []*node
	expanded bool
}

// visRow is a flattened, indented view row.
type visRow struct {
	n     *node
	depth int
}

type treeModel struct {
	ready    bool
	seeded   bool   // selection has been seeded once (survives re-entry)
	notice   string // transient validation message (e.g. "select at least one")
	root     *node
	rows     []visRow
	cursor   int
	offset   int
	listView bool

	filtering bool
	filter    textinput.Model

	preview        viewport.Model
	previewFocused bool
	glam           *glamour.TermRenderer
	gen            int
	previewW       int
}

func skillKey(sk *engine.Skill) string {
	if sk.RelPath == "" {
		return sk.Name
	}
	return sk.RelPath
}

func (s *treeModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.filter = textinput.New()
		s.filter.Prompt = "/ "
		s.filter.Placeholder = "filter skills"
		s.preview = viewport.New(0, 0)
		s.ready = true
	}
	s.listView = app.prefs.View == config.ViewList
	// Build the tree (and seed the selection) only once, so returning from the
	// agent/review screens preserves the user's picks and expand/collapse state.
	if s.root == nil {
		s.root = buildTree(app.discovered.Skills)
	}
	if !s.seeded {
		// Default: everything selected (an interactive equivalent of --all).
		for i := range app.discovered.Skills {
			app.selection.set(skillKey(&app.discovered.Skills[i]), true)
		}
		s.cursor = 0
		s.seeded = true
	}
	s.layout(app)
	s.rebuild(app)
	return s.loadPreview(app)
}

func (s *treeModel) capturing() bool { return s.filtering }

// layout (re)sizes the preview pane and glamour renderer for the current width.
func (s *treeModel) layout(app *App) {
	leftW := app.width * 9 / 20
	if leftW < 20 {
		leftW = 20
	}
	rightW := app.width - leftW - 3
	if rightW < 10 {
		rightW = 10
	}
	h := app.bodyHeight() - 4
	if h < 1 {
		h = 1
	}
	s.preview.Width = rightW
	s.preview.Height = h
	if s.previewW != rightW || s.glam == nil {
		s.previewW = rightW
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(app.glamStyle), glamour.WithWordWrap(rightW))
		if err == nil {
			s.glam = r
		}
	}
}

func (s *treeModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.layout(app)
		return nil

	case skillDocMsg:
		if msg.gen != s.gen {
			return nil // stale
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
		if fm := strings.TrimSpace(msg.doc.Skill.FrontmatterRaw); fm != "" {
			body = frontmatterBox(app, fm, s.preview.Width) + "\n\n" + body
		}
		s.preview.SetContent(body)
		s.preview.GotoTop()
		return nil

	case tea.KeyMsg:
		return s.handleKey(app, msg)
	}
	return nil
}

func (s *treeModel) handleKey(app *App, msg tea.KeyMsg) tea.Cmd {
	if s.filtering {
		switch msg.String() {
		case "esc":
			s.filtering = false
			s.filter.Blur()
			s.filter.SetValue("")
			s.rebuild(app)
			return s.loadPreview(app)
		case "enter":
			s.filtering = false
			s.filter.Blur()
			return nil
		}
		var cmd tea.Cmd
		s.filter, cmd = s.filter.Update(msg)
		s.rebuild(app)
		return tea.Batch(cmd, s.loadPreview(app))
	}

	// Preview focus: route scroll keys to the viewport; tab/esc/← return to the list.
	if s.previewFocused {
		switch msg.String() {
		case "tab", "esc", "left", "h":
			s.previewFocused = false
			return nil
		}
		var cmd tea.Cmd
		s.preview, cmd = s.preview.Update(msg)
		return cmd
	}

	s.notice = "" // any action key clears a stale validation notice

	switch {
	case key.Matches(msg, app.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
		s.clampOffset(app)
		return s.loadPreview(app)
	case key.Matches(msg, app.keys.Down):
		if s.cursor < len(s.rows)-1 {
			s.cursor++
		}
		s.clampOffset(app)
		return s.loadPreview(app)
	case msg.String() == "tab":
		// Focus the preview to scroll it (only when previewing a skill).
		if n := s.curNode(); n != nil && n.isSkill {
			s.previewFocused = true
		}
		return nil
	case key.Matches(msg, app.keys.Filter):
		s.filtering = true
		return s.filter.Focus()
	case key.Matches(msg, app.keys.View):
		s.listView = !s.listView
		app.prefs.View = viewName(s.listView)
		_ = app.prefs.Save()
		s.cursor = 0
		s.rebuild(app)
		return s.loadPreview(app)
	case key.Matches(msg, app.keys.All):
		s.selectAll(app, true)
		return nil
	case key.Matches(msg, app.keys.None):
		s.selectAll(app, false)
		return nil
	case key.Matches(msg, app.keys.Toggle):
		s.toggleCursor(app)
		return nil
	case key.Matches(msg, app.keys.Collapse):
		s.collapseOrParent(app)
		return s.loadPreview(app)
	case msg.String() == "right" || msg.String() == "l":
		s.expandCursor(app)
		return nil
	case key.Matches(msg, app.keys.Confirm):
		if app.selection.count() == 0 {
			s.notice = "select at least one skill to continue"
			return nil
		}
		return func() tea.Msg { return nav(stateAgents) }
	case key.Matches(msg, app.keys.OpenNPMX):
		if app.src.Kind == spec.KindNPM && app.src.Pkg != "" {
			url := "https://npmx.dev/package/" + app.src.Pkg
			return func() tea.Msg {
				openBrowser(url)
				return nil
			}
		}
		return nil
	case key.Matches(msg, app.keys.Back):
		return func() tea.Msg { return navBackMsg{} }
	}
	return nil
}

func (s *treeModel) curNode() *node {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return nil
	}
	return s.rows[s.cursor].n
}

func (s *treeModel) toggleCursor(app *App) {
	n := s.curNode()
	if n == nil {
		return
	}
	if n.isSkill {
		app.selection.toggle(n.key)
		return
	}
	// Folder: if fully checked, clear all descendants; otherwise select all.
	st := folderState(n, app.selection)
	leaves := descendantSkills(n)
	for _, l := range leaves {
		app.selection.set(l.key, st != checked)
	}
}

func (s *treeModel) expandCursor(app *App) {
	if n := s.curNode(); n != nil && !n.isSkill && !n.expanded {
		n.expanded = true
		s.rebuild(app)
	}
}

// collapseOrParent collapses an expanded folder under the cursor; on a skill or
// already-collapsed folder it moves the cursor to the parent row instead.
func (s *treeModel) collapseOrParent(app *App) {
	n := s.curNode()
	if n == nil {
		return
	}
	if !n.isSkill && n.expanded {
		n.expanded = false
		s.rebuild(app)
		return
	}
	depth := s.rows[s.cursor].depth
	for i := s.cursor - 1; i >= 0; i-- {
		if s.rows[i].depth < depth {
			s.cursor = i
			s.clampOffset(app)
			return
		}
	}
}

func (s *treeModel) selectAll(app *App, on bool) {
	for _, l := range descendantSkills(s.root) {
		app.selection.set(l.key, on)
	}
}

// loadPreview fires an async SKILL.md read for the skill under the cursor.
func (s *treeModel) loadPreview(app *App) tea.Cmd {
	n := s.curNode()
	if n == nil || !n.isSkill || n.skill == nil {
		s.preview.SetContent(app.th.faint.Render("(select a skill to preview)"))
		return nil
	}
	s.gen++
	gen := s.gen
	sk := *n.skill
	eng := app.eng
	return func() tea.Msg {
		doc, err := eng.ReadSkillDoc(sk)
		return skillDocMsg{gen: gen, doc: doc, err: err, path: sk.RelPath}
	}
}

// rebuild recomputes the visible rows for the active view and filter, keeping
// the cursor in range.
func (s *treeModel) rebuild(app *App) {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	if s.listView {
		s.rows = flatRows(app.discovered.Skills, q)
	} else {
		match := matchSet(s.root, q)
		var rows []visRow
		flatten(s.root, 0, match, q != "", &rows)
		s.rows = rows
	}
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
	s.clampOffset(app)
}

func (s *treeModel) clampOffset(app *App) {
	visible := s.listHeight(app)
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+visible {
		s.offset = s.cursor - visible + 1
	}
	if s.offset < 0 {
		s.offset = 0
	}
}

func (s *treeModel) listHeight(app *App) int {
	h := app.bodyHeight() - 4
	if h < 1 {
		h = 1
	}
	return h
}

func (s *treeModel) view(app *App) string {
	left := s.leftPane(app)
	borderStyle := app.th.border
	if s.previewFocused {
		borderStyle = app.th.borderFocus
	}
	right := borderStyle.Render(s.preview.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)

	count := app.selection.count()
	status := app.th.badge.Render(fmt.Sprintf("%d selected", count))
	if n := len(app.discovered.References); n > 0 {
		status += app.th.faint.Render(fmt.Sprintf("  · +%d reference(s)", n))
	}
	if s.notice != "" {
		status += "  " + app.th.errorText.Render(s.notice)
	}
	var help string
	switch {
	case s.filtering:
		help = app.helpView(app.keys.Confirm, app.keys.Back)
	case s.previewFocused:
		help = app.helpView(
			app.keys.Up, app.keys.Down,
			key.NewBinding(key.WithHelp("esc", "back to list")),
		)
	default:
		bindings := []key.Binding{
			app.keys.Toggle, app.keys.Expand, app.keys.Collapse,
			key.NewBinding(key.WithHelp("tab", "scroll preview")),
			app.keys.View, app.keys.Filter, app.keys.Confirm,
		}
		if app.src.Kind == spec.KindNPM && app.src.Pkg != "" {
			bindings = append(bindings, app.keys.OpenNPMX)
		}
		help = app.helpView(bindings...)
	}
	return body + "\n" + status + "\n" + help
}

func (s *treeModel) leftPane(app *App) string {
	var b strings.Builder
	if s.filtering || s.filter.Value() != "" {
		fmt.Fprintf(&b, "%s\n", s.filter.View())
	} else {
		view := "tree"
		if s.listView {
			view = "list"
		}
		fmt.Fprintf(&b, "%s\n", app.th.subtitle.Render("skills ("+view+" view)"))
	}

	visible := s.listHeight(app)
	end := s.offset + visible
	if end > len(s.rows) {
		end = len(s.rows)
	}
	for i := s.offset; i < end; i++ {
		fmt.Fprintf(&b, "%s\n", s.rowView(app, s.rows[i], i == s.cursor))
	}
	if len(s.rows) == 0 {
		fmt.Fprintf(&b, "%s\n", app.th.faint.Render("  (no matching skills)"))
	}
	return b.String()
}

func (s *treeModel) rowView(app *App, r visRow, atCursor bool) string {
	n := r.n
	indent := strings.Repeat("  ", r.depth)

	var box string
	if n.isSkill {
		box = checkbox(app, boolState(app.selection.has(n.key)))
	} else {
		box = checkbox(app, folderState(n, app.selection))
	}

	var label string
	switch {
	case n.isSkill:
		label = n.label
	case n.expanded:
		label = "▾ " + n.label
	default:
		label = "▸ " + n.label
	}

	cursor := "  "
	if atCursor {
		cursor = app.th.cursor.Render("▸ ")
		label = app.th.selected.Render(label)
	}
	line := cursor + indent + box + " " + label
	if s.listView && n.isSkill && n.skill != nil && n.skill.Description != "" {
		line += app.th.faint.Render("  — " + n.skill.Description)
	}
	return truncate(line, leftWidth(app))
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func leftWidth(app *App) int {
	w := app.width*9/20 - 1
	if w < 20 {
		w = 20
	}
	return w
}

func checkbox(app *App, st checkState) string {
	switch st {
	case checked:
		return app.th.check.Render("[x]")
	case partial:
		return app.th.partial.Render("[~]")
	default:
		return "[ ]"
	}
}

func boolState(on bool) checkState {
	if on {
		return checked
	}
	return unchecked
}

func viewName(list bool) string {
	if list {
		return config.ViewList
	}
	return config.ViewTree
}

// ---- tree construction & traversal -----------------------------------------

func buildTree(skills []engine.Skill) *node {
	root := &node{expanded: true}
	for i := range skills {
		sk := &skills[i]
		if sk.RelPath == "" {
			root.children = append(root.children, &node{label: sk.Name, key: skillKey(sk), isSkill: true, skill: sk})
			continue
		}
		segs := strings.Split(sk.RelPath, "/")
		cur := root
		for j, seg := range segs {
			leaf := j == len(segs)-1
			child := findChild(cur, seg)
			if child == nil {
				child = &node{label: seg, expanded: true}
				cur.children = append(cur.children, child)
			}
			if leaf {
				child.isSkill = true
				child.skill = sk
				child.key = skillKey(sk)
			}
			cur = child
		}
	}
	sortNode(root)
	return root
}

func findChild(n *node, label string) *node {
	for _, c := range n.children {
		if c.label == label {
			return c
		}
	}
	return nil
}

func sortNode(n *node) {
	sort.Slice(n.children, func(i, j int) bool {
		// Folders before skills, then alphabetical.
		a, b := n.children[i], n.children[j]
		if a.isSkill != b.isSkill {
			return !a.isSkill
		}
		return a.label < b.label
	})
	for _, c := range n.children {
		sortNode(c)
	}
}

func descendantSkills(n *node) []*node {
	var out []*node
	var walk func(*node)
	walk = func(x *node) {
		if x.isSkill {
			out = append(out, x)
			return
		}
		for _, c := range x.children {
			walk(c)
		}
	}
	walk(n)
	return out
}

func folderState(n *node, sel *skillSet) checkState {
	leaves := descendantSkills(n)
	if len(leaves) == 0 {
		return unchecked
	}
	on := 0
	for _, l := range leaves {
		if sel.has(l.key) {
			on++
		}
	}
	switch {
	case on == 0:
		return unchecked
	case on == len(leaves):
		return checked
	default:
		return partial
	}
}

// matchSet returns the set of nodes to keep for the filter query: matching
// skills and all their ancestors. Empty query => nil (keep everything).
func matchSet(root *node, q string) map[*node]bool {
	if q == "" {
		return nil
	}
	keep := map[*node]bool{}
	var walk func(*node) bool
	walk = func(n *node) bool {
		anyChild := false
		for _, c := range n.children {
			if walk(c) {
				anyChild = true
			}
		}
		self := n.isSkill && nodeMatches(n, q)
		if self || anyChild {
			keep[n] = true
			return true
		}
		return false
	}
	walk(root)
	return keep
}

func nodeMatches(n *node, q string) bool {
	if strings.Contains(strings.ToLower(n.label), q) {
		return true
	}
	if n.skill != nil {
		if strings.Contains(strings.ToLower(n.skill.Name), q) ||
			strings.Contains(strings.ToLower(n.skill.Description), q) ||
			strings.Contains(strings.ToLower(n.skill.RelPath), q) {
			return true
		}
	}
	return false
}

// flatten appends visible rows for the tree. When filtering, only kept nodes are
// shown and folders are force-expanded.
func flatten(n *node, depth int, keep map[*node]bool, filtering bool, out *[]visRow) {
	for _, c := range n.children {
		if keep != nil && !keep[c] {
			continue
		}
		*out = append(*out, visRow{n: c, depth: depth})
		if c.isSkill {
			continue
		}
		if c.expanded || filtering {
			flatten(c, depth+1, keep, filtering, out)
		}
	}
}

// flatRows builds the flat-list view: one row per skill, optionally filtered.
func flatRows(skills []engine.Skill, q string) []visRow {
	var out []visRow
	for i := range skills {
		sk := &skills[i]
		n := &node{label: flatLabel(sk), key: skillKey(sk), isSkill: true, skill: sk}
		if q != "" && !nodeMatches(n, q) {
			continue
		}
		out = append(out, visRow{n: n, depth: 0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n.label < out[j].n.label })
	return out
}

func flatLabel(sk *engine.Skill) string {
	if sk.RelPath == "" {
		return sk.Name
	}
	return sk.RelPath
}
