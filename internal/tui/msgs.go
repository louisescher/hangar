package tui

import (
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/install"
)

// state enumerates the screens of the install flow.
type state int

const (
	stateCatalog state = iota
	stateCrawling
	stateTree
	stateAgents
	stateOptions
	stateConfirm
	stateInstalling
	stateResults
	stateManage
	stateInfoList
	stateInfoDetail
	stateProfileList
	stateProfileDetail
)

// navMsg requests a forward transition to another screen. Screens emit it; the
// root pushes the current screen onto the back-stack, then transitions (running
// the target's enter()).
type navMsg struct{ to state }

func nav(to state) navMsg { return navMsg{to: to} }

// navBackMsg pops the back-stack and returns to the previous screen. With an
// empty stack (the flow's entry screen) it quits. This is what every "esc/back"
// binding emits, so back always retraces the actual path taken — including
// detours via the review screen's a/s/o shortcuts.
type navBackMsg struct{}

// navReplaceMsg switches screens without touching the back-stack. Used for async
// progressions (crawl→tree, install→results) so a transient screen never becomes
// a back target.
type navReplaceMsg struct{ to state }

func navReplace(to state) navReplaceMsg { return navReplaceMsg{to: to} }

// errMsg carries a failure to the root, which surfaces it on the active screen.
type errMsg struct{ err error }

// discoveredMsg delivers the result of an async crawl.
type discoveredMsg struct{ d *engine.Discovered }

// skillDocMsg delivers an async SKILL.md read for the preview pane. gen guards
// against stale results when the cursor has moved on.
type skillDocMsg struct {
	gen  int
	doc  engine.SkillDoc
	err  error
	path string
}

// installDoneMsg delivers the install report (or error) from the async install.
type installDoneMsg struct {
	report install.Report
	err    error
}

// manageStatusMsg delivers the async update-check for installed entries.
type manageStatusMsg struct{ statuses []engine.InstalledStatus }

// manageOpMsg signals that an update/remove operation finished. kind is
// "remove", "update", or "updateAll"; name is the affected entry ("" for all).
type manageOpMsg struct {
	kind string
	name string
	note string
	err  error
}

// manageDiffMsg delivers an async update-diff preview.
type manageDiffMsg struct {
	name string
	diff string
	err  error
}

// progressMsg delivers one install progress event from the bridge channel.
type progressMsg struct {
	ev install.Event
	ok bool
}
