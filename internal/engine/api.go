package engine

import (
	"os"
	"path/filepath"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/discover"
)

// Agent is the resolved-agent type surfaced to callers (CLI and TUI).
type Agent = agents.Agent

// SkillDoc is a skill's manifest split for display: the parsed skill metadata
// plus the markdown body (frontmatter removed), ready for glamour rendering.
type SkillDoc struct {
	Skill Skill
	Body  string
}

// ReadSkillDoc loads a skill's manifest (SKILL.md, falling back to REFERENCE.md
// for installed references) and returns its body for preview. The skill's
// AbsPath must still be on disk (true while its source's Discovered is open).
func (e *Engine) ReadSkillDoc(sk Skill) (SkillDoc, error) {
	data, err := os.ReadFile(filepath.Join(sk.AbsPath, "SKILL.md"))
	if err != nil {
		var refErr error
		data, refErr = os.ReadFile(filepath.Join(sk.AbsPath, "REFERENCE.md"))
		if refErr != nil {
			return SkillDoc{}, err
		}
	}
	_, body := discover.SplitFrontmatter(data)
	return SkillDoc{Skill: sk, Body: string(body)}, nil
}

// DetectAgents returns the agents detected on this machine for the given scope.
func (e *Engine) DetectAgents(global bool) ([]agents.Agent, error) {
	return agents.Detect(global)
}

// ResolveAgents resolves agent names (and aliases) to concrete install targets.
func (e *Engine) ResolveAgents(names []string, global bool) ([]agents.Agent, error) {
	out, _, err := agents.Resolve(names, global)
	return out, err
}

// AgentDefs returns the full static agent matrix (for selection UIs).
func (e *Engine) AgentDefs() []agents.Def {
	return agents.Defs
}

