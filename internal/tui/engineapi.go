// Package tui implements Hangar's interactive terminal UI: a browse → search →
// crawl → pick skills → pick agents → confirm → install → results flow built on
// bubbletea. Screens talk to the rest of Hangar only through EngineAPI, so they
// can be driven by a fake in tests.
package tui

import (
	"context"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/spec"
)

// EngineAPI is the surface the TUI depends on. *engine.Engine satisfies it; a
// fake implements it in tests.
type EngineAPI interface {
	// Discover fetches a source and crawls it for skills and references. The
	// returned *engine.Discovered must be Closed by the caller.
	Discover(ctx context.Context, s spec.SourceSpec) (*engine.Discovered, error)
	// ReadSkillDoc loads a skill's SKILL.md body for the preview pane.
	ReadSkillDoc(sk engine.Skill) (engine.SkillDoc, error)
	// DetectAgents lists agents present on this machine for the given scope.
	DetectAgents(global bool) ([]agents.Agent, error)
	// ResolveAgents resolves agent names to concrete install targets.
	ResolveAgents(names []string, global bool) ([]agents.Agent, error)
	// AgentDefs returns the full agent matrix for the selection screen.
	AgentDefs() []agents.Def
	// InstallSelected installs the chosen skills/references into the chosen
	// agents. d must still be open.
	InstallSelected(d *engine.Discovered, s spec.SourceSpec, skills []engine.Skill, refs []discover.RefDoc, agts []agents.Agent, opt engine.InstallOptions) (install.Report, error)

	// Installed returns the lockfile entries for the manage screen.
	Installed(global bool) ([]lockfile.Entry, error)
	// CheckUpdates resolves which installed entries are outdated.
	CheckUpdates(ctx context.Context, global bool) ([]engine.InstalledStatus, error)
	// Update re-resolves and reinstalls an installed skill ("" = all).
	Update(ctx context.Context, name string, opt engine.InstallOptions) (install.Report, error)
	// UpdateNames updates exactly the named entries — used by "update all
	// outdated", which already knows which are stale from the cached status.
	UpdateNames(ctx context.Context, names []string, opt engine.InstallOptions) (install.Report, error)
	// Remove deletes an installed skill/reference everywhere.
	Remove(name string, opt engine.InstallOptions) error
	// PreviewUpdate returns a unified diff of a pending update (empty = no change).
	PreviewUpdate(ctx context.Context, name string, opt engine.InstallOptions) (string, error)
	// SetPinned records an entry's pinned flag without reinstalling.
	SetPinned(name string, pinned bool, opt engine.InstallOptions) error
}
