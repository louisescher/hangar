package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/lockfile"
)

// manage table column widths.
const (
	mgNameW   = 22
	mgKindW   = 6
	mgStatusW = 14
)

// manageRow is one installed entry plus its (lazily fetched) update status.
type manageRow struct {
	entry  lockfile.Entry
	status *engine.InstalledStatus
}

// manageModel is the `hangar list` (no source) screen: it lists installed
// skills/references, flags outdated ones, and updates or removes them in place.
type manageModel struct {
	ready         bool
	rows          []manageRow
	cursor, off   int
	sp            spinner.Model
	checking      bool // async update-check in flight
	busy          bool // an update/remove op in flight
	busyMsg       string
	confirmRemove string // entry name pending remove confirmation
	note          string // transient status line

	// statusCache holds the last-known update status per entry name so removing
	// or updating one entry doesn't force a re-check of the others.
	statusCache map[string]engine.InstalledStatus

	previewing  bool // showing an update diff
	previewName string
	preview     viewport.Model
}

func (s *manageModel) enter(app *App) tea.Cmd {
	if !s.ready {
		s.sp = spinner.New()
		s.sp.Spinner = spinner.Dot
		s.preview = viewport.New(0, 0)
		s.statusCache = map[string]engine.InstalledStatus{}
		s.ready = true
	}
	s.previewing = false
	s.confirmRemove = ""
	s.note = ""
	if err := s.reload(app); err != nil {
		app.err = err
		return nil
	}
	if len(s.rows) == 0 {
		return nil
	}
	s.checking = true
	ctx := app.startOp()
	return tea.Batch(s.sp.Tick, manageCheckCmd(ctx, app.eng, app.global))
}

func (s *manageModel) reload(app *App) error {
	entries, err := app.eng.Installed(app.global)
	if err != nil {
		return err
	}
	rows := make([]manageRow, len(entries))
	for i, e := range entries {
		rows[i] = manageRow{entry: e}
	}
	s.rows = rows
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
	s.attachFromCache()
	return nil
}

// attachFromCache points each row at its cached status (nil if none).
func (s *manageModel) attachFromCache() {
	for i := range s.rows {
		if st, ok := s.statusCache[s.rows[i].entry.Name]; ok {
			cp := st
			s.rows[i].status = &cp
		} else {
			s.rows[i].status = nil
		}
	}
}

// capturing suppresses global hotkeys (notably q) while confirming a removal,
// running an operation, or viewing a diff.
func (s *manageModel) capturing() bool {
	return s.busy || s.confirmRemove != "" || s.previewing
}

func (s *manageModel) update(app *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if s.checking || s.busy {
			var cmd tea.Cmd
			s.sp, cmd = s.sp.Update(msg)
			return cmd
		}
		return nil

	case manageStatusMsg:
		s.checking = false
		s.attach(msg.statuses)
		return nil

	case manageDiffMsg:
		s.busy = false
		if msg.err != nil {
			s.note = "error: " + msg.err.Error()
			return nil
		}
		if strings.TrimSpace(msg.diff) == "" {
			s.note = "no pending changes for " + msg.name
			return nil
		}
		s.previewName = msg.name
		s.preview.Width = maxInt(app.width-4, 10)
		s.preview.Height = maxInt(app.bodyHeight()-4, 3)
		s.preview.SetContent(colorizeDiff(app, msg.diff))
		s.preview.GotoTop()
		s.previewing = true
		return nil

	case manageOpMsg:
		s.busy = false
		if msg.err != nil {
			s.note = "error: " + msg.err.Error()
			return nil
		}
		if msg.note != "" {
			s.note = msg.note
		}
		// Surgically update the cache so unaffected rows keep their known
		// status — no full re-check (and no network) on remove/update.
		switch msg.kind {
		case "remove":
			delete(s.statusCache, msg.name)
		case "update":
			s.markUpToDate(app, msg.name)
		case "updateAll":
			s.markUpToDate(app, "")
		}
		if err := s.reload(app); err != nil {
			app.err = err
		}
		return nil

	case tea.KeyMsg:
		return s.handleKey(app, msg)
	}
	return nil
}

func (s *manageModel) handleKey(app *App, msg tea.KeyMsg) tea.Cmd {
	if s.previewing {
		switch msg.String() {
		case "esc", "q", "enter":
			s.previewing = false
			return nil
		}
		var cmd tea.Cmd
		s.preview, cmd = s.preview.Update(msg)
		return cmd
	}
	if s.busy {
		return nil // ignore input during an operation
	}
	if s.confirmRemove != "" {
		switch msg.String() {
		case "y":
			name := s.confirmRemove
			s.confirmRemove = ""
			s.busy = true
			s.busyMsg = "removing " + name
			return tea.Batch(s.sp.Tick, manageRemoveCmd(app.eng, name, app.global))
		default: // n, esc, anything else cancels
			s.confirmRemove = ""
			s.note = ""
			return nil
		}
	}

	switch {
	case key.Matches(msg, app.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(msg, app.keys.Down):
		if s.cursor < len(s.rows)-1 {
			s.cursor++
		}
	case msg.String() == "u":
		return s.updateOne(app)
	case msg.String() == "U":
		return s.updateAll(app)
	case msg.String() == "x" || msg.String() == "d":
		if r := s.cur(); r != nil {
			s.confirmRemove = r.entry.Name
			s.note = ""
		}
	case msg.String() == "p":
		return s.togglePin(app)
	case key.Matches(msg, app.keys.Confirm): // enter → preview the pending diff
		return s.previewDiff(app)
	case msg.String() == "r":
		return s.enter(app)
	case key.Matches(msg, app.keys.Back):
		return func() tea.Msg { return nav(stateCatalog) }
	}
	return nil
}

func (s *manageModel) togglePin(app *App) tea.Cmd {
	r := s.cur()
	if r == nil {
		return nil
	}
	pinned := !r.entry.Pinned
	if err := app.eng.SetPinned(r.entry.Name, pinned, engine.InstallOptions{Global: app.global}); err != nil {
		s.note = "error: " + err.Error()
		return nil
	}
	verb := "pinned"
	if !pinned {
		verb = "unpinned"
	}
	s.note = verb + " " + r.entry.Name
	_ = s.reload(app)
	return nil
}

func (s *manageModel) previewDiff(app *App) tea.Cmd {
	r := s.cur()
	if r == nil {
		return nil
	}
	if r.entry.Pinned {
		s.note = r.entry.Name + " is pinned — nothing to update"
		return nil
	}
	s.busy = true
	s.busyMsg = "computing diff for " + r.entry.Name
	name := r.entry.Name
	ctx := app.startOp()
	return tea.Batch(s.sp.Tick, func() tea.Msg {
		d, err := app.eng.PreviewUpdate(ctx, name, engine.InstallOptions{Global: app.global, Security: app.security})
		return manageDiffMsg{name: name, diff: d, err: err}
	})
}

func (s *manageModel) cur() *manageRow {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return nil
	}
	return &s.rows[s.cursor]
}

func (s *manageModel) updateOne(app *App) tea.Cmd {
	r := s.cur()
	if r == nil {
		return nil
	}
	s.busy = true
	s.busyMsg = "updating " + r.entry.Name
	ctx := app.startOp()
	return tea.Batch(s.sp.Tick, manageUpdateCmd(ctx, app.eng, r.entry.Name, app.global))
}

func (s *manageModel) updateAll(app *App) tea.Cmd {
	// Use the cached status: update exactly the entries already known to be
	// outdated, without re-resolving the rest (a re-check needs an explicit 'r').
	var names []string
	for i := range s.rows {
		if st := s.rows[i].status; st != nil && st.Outdated {
			names = append(names, s.rows[i].entry.Name)
		}
	}
	if len(names) == 0 {
		s.note = "nothing outdated (press r to re-check)"
		return nil
	}
	s.busy = true
	s.busyMsg = fmt.Sprintf("updating %d outdated", len(names))
	ctx := app.startOp()
	return tea.Batch(s.sp.Tick, manageUpdateNamesCmd(ctx, app.eng, names, app.global))
}

// attach records a full update-check into the cache and re-points the rows.
func (s *manageModel) attach(statuses []engine.InstalledStatus) {
	if s.statusCache == nil {
		s.statusCache = map[string]engine.InstalledStatus{}
	}
	for _, st := range statuses {
		s.statusCache[st.Entry.Name] = st
	}
	s.attachFromCache()
}

// markUpToDate caches the named entry (or all, when name == "") as current,
// using its freshly reinstalled ref/version — avoiding a network re-check.
func (s *manageModel) markUpToDate(app *App, name string) {
	entries, err := app.eng.Installed(app.global)
	if err != nil {
		return
	}
	for _, e := range entries {
		if name == "" || e.Name == name {
			s.statusCache[e.Name] = engine.InstalledStatus{Entry: e, Outdated: false, Latest: entryRefLabel(e)}
		}
	}
}

func (s *manageModel) view(app *App) string {
	if s.previewing {
		var b strings.Builder
		fmt.Fprintf(&b, "%s\n", app.th.title.Render("Pending update — "+s.previewName))
		fmt.Fprintf(&b, "%s\n", app.th.border.Render(s.preview.View()))
		b.WriteString(app.helpView(
			app.keys.Up, app.keys.Down,
			key.NewBinding(key.WithHelp("esc", "close")),
		))
		return b.String()
	}

	var b strings.Builder
	scope := "this project"
	if app.global {
		scope = "global (~/)"
	}
	fmt.Fprintf(&b, "%s\n\n", app.th.subtitle.Render("Installed skills — "+scope))

	if len(s.rows) == 0 {
		fmt.Fprintf(&b, "%s\n\n", app.th.faint.Render("  nothing installed yet — run `hangar install <source>`"))
		b.WriteString(app.helpView(app.keys.Back, app.keys.Quit))
		return b.String()
	}

	// Table header.
	fmt.Fprintf(&b, "%s\n", app.th.faint.Render(fmt.Sprintf("  %-*s %-*s %-*s %s",
		mgNameW, "NAME", mgKindW, "KIND", mgStatusW, "STATUS", "SOURCE")))

	visible := app.bodyHeight() - 6
	if visible < 1 {
		visible = 1
	}
	if s.cursor < s.off {
		s.off = s.cursor
	}
	if s.cursor >= s.off+visible {
		s.off = s.cursor - visible + 1
	}
	end := s.off + visible
	if end > len(s.rows) {
		end = len(s.rows)
	}
	for i := s.off; i < end; i++ {
		fmt.Fprintf(&b, "%s\n", s.rowView(app, s.rows[i], i == s.cursor))
	}

	b.WriteString("\n")
	switch {
	case s.busy:
		fmt.Fprintf(&b, "%s %s\n", s.sp.View(), app.th.faint.Render(s.busyMsg))
	case s.checking:
		fmt.Fprintf(&b, "%s %s\n", s.sp.View(), app.th.faint.Render("checking for updates…"))
	case s.confirmRemove != "":
		fmt.Fprintf(&b, "%s%s\n", app.th.errorText.Render("remove "+s.confirmRemove+"? "), app.th.faint.Render("(y to confirm, n to cancel)"))
	case s.note != "":
		fmt.Fprintf(&b, "%s\n", app.th.okText.Render(s.note))
	default:
		b.WriteString("\n")
	}

	b.WriteString(app.helpView(
		app.keys.Up, app.keys.Down,
		key.NewBinding(key.WithHelp("⏎", "diff")),
		key.NewBinding(key.WithHelp("u", "update")),
		key.NewBinding(key.WithHelp("U", "all")),
		key.NewBinding(key.WithHelp("p", "pin")),
		key.NewBinding(key.WithHelp("x", "remove")),
		key.NewBinding(key.WithHelp("r", "refresh")),
		app.keys.Quit,
	))
	return b.String()
}

// colorizeDiff applies +/- /@ coloring to a unified diff for the preview pane.
func colorizeDiff(app *App, d string) string {
	var b strings.Builder
	for _, ln := range strings.Split(d, "\n") {
		switch {
		case strings.HasPrefix(ln, "+"):
			b.WriteString(app.th.okText.Render(ln))
		case strings.HasPrefix(ln, "-"):
			b.WriteString(app.th.errorText.Render(ln))
		case strings.HasPrefix(ln, "@"):
			b.WriteString(app.th.accent.Render(ln))
		default:
			b.WriteString(app.th.faint.Render(ln))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *manageModel) rowView(app *App, r manageRow, atCursor bool) string {
	cursor := "  "
	if atCursor {
		cursor = app.th.cursor.Render("▸ ")
	}

	name := fmt.Sprintf("%-*s", mgNameW, truncate(r.entry.Name, mgNameW))
	if atCursor {
		name = app.th.selected.Render(name)
	}

	kind := "skill"
	if r.entry.Kind == lockfile.KindRef {
		kind = "ref"
	}

	statusText, statusStyle := s.statusCell(app, r)
	status := statusStyle.Render(fmt.Sprintf("%-*s", mgStatusW, truncate(statusText, mgStatusW)))

	srcRef := r.entry.Source
	if v := entryRefLabel(r.entry); v != "" {
		srcRef += "@" + v
	}
	srcW := app.width - (2 + mgNameW + 1 + mgKindW + 1 + mgStatusW + 1)
	src := app.th.faint.Render(truncate(srcRef, maxInt(srcW, 6)))

	return fmt.Sprintf("%s%s %-*s %s %s", cursor, name, mgKindW, kind, status, src)
}

// statusCell returns the status column's plain text and its style.
func (s *manageModel) statusCell(app *App, r manageRow) (string, lipgloss.Style) {
	switch {
	case r.status != nil && r.status.Gone:
		return "deleted", app.th.errorText
	case r.entry.Pinned:
		return "pinned", app.th.faint
	case r.status == nil:
		return "…", app.th.faint
	case r.status.Err != "":
		return "?", app.th.faint
	case r.status.Outdated:
		return "⬆ " + r.status.Latest, app.th.badge
	default:
		return "up to date", app.th.okText
	}
}

// entryRefLabel is the version (npm) or ref (GitHub) shown for an entry.
func entryRefLabel(e lockfile.Entry) string {
	if e.Version != "" {
		return e.Version
	}
	return e.Ref
}

// ---- commands ---------------------------------------------------------------

func manageCheckCmd(ctx context.Context, eng EngineAPI, global bool) tea.Cmd {
	return func() tea.Msg {
		st, err := eng.CheckUpdates(ctx, global)
		if err != nil {
			return errMsg{err}
		}
		return manageStatusMsg{statuses: st}
	}
}

func manageUpdateCmd(ctx context.Context, eng EngineAPI, name string, global bool) tea.Cmd {
	return func() tea.Msg {
		rep, err := eng.Update(ctx, name, engine.InstallOptions{Global: global})
		if err != nil {
			return manageOpMsg{kind: "update", name: name, err: err}
		}
		if len(rep.Gone) > 0 {
			return manageOpMsg{kind: "update", name: name, note: fmt.Sprintf("%s no longer exists upstream — kept; press x to remove", rep.Gone[0])}
		}
		if len(rep.Skills) == 0 {
			return manageOpMsg{kind: "update", name: name, note: "already up to date"}
		}
		return manageOpMsg{kind: "update", name: name, note: fmt.Sprintf("updated %d item(s)", len(rep.Skills))}
	}
}

// manageUpdateNamesCmd updates a known set of outdated entries (from the cache).
func manageUpdateNamesCmd(ctx context.Context, eng EngineAPI, names []string, global bool) tea.Cmd {
	return func() tea.Msg {
		rep, err := eng.UpdateNames(ctx, names, engine.InstallOptions{Global: global})
		if err != nil {
			return manageOpMsg{kind: "updateAll", err: err}
		}
		note := fmt.Sprintf("updated %d item(s)", len(rep.Skills))
		if len(rep.Gone) > 0 {
			note += fmt.Sprintf(" · %d deleted upstream (kept)", len(rep.Gone))
		}
		return manageOpMsg{kind: "updateAll", note: note}
	}
}

func manageRemoveCmd(eng EngineAPI, name string, global bool) tea.Cmd {
	return func() tea.Msg {
		if err := eng.Remove(name, engine.InstallOptions{Global: global}); err != nil {
			return manageOpMsg{kind: "remove", name: name, err: err}
		}
		return manageOpMsg{kind: "remove", name: name, note: "removed " + name}
	}
}
