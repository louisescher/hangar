// Package agents holds the built-in matrix of AI coding agents Hangar can
// install skills into, and the logic to detect which are present on a machine.
//
// Each Def carries two install locations: ProjectPath (relative, used when
// installing into a repository) and GlobalPath (joined under $HOME, used with
// --global). DetectDir is the per-user directory probed by auto-detection; an
// empty DetectDir marks a target-only agent (e.g. "universal") that must be
// requested explicitly with -a.
//
// The table is ported from withastro/rosie (src/agent.rs / agent.ts).
package agents

import (
	"fmt"
	"os"
	"path/filepath"
)

// Def is a static definition of a supported agent.
type Def struct {
	Name        string
	Display     string
	Aliases     []string
	ProjectPath string
	GlobalPath  string
	DetectDir   string // empty => target-only, never auto-detected
	Binary      string // optional CLI binary name; "" if none
}

// Agent is a Def resolved to a concrete install location for one invocation.
type Agent struct {
	Def         Def
	InstallPath string // relative for local installs, absolute (under $HOME) for global
	Detected    bool
}

// Defs is the full agent matrix.
var Defs = []Def{
	// --- Original 12 ---
	{Name: "claude", Display: "Claude Code", ProjectPath: ".claude/skills", GlobalPath: ".claude/skills", DetectDir: ".claude", Binary: "claude"},
	{Name: "cursor", Display: "Cursor", ProjectPath: ".cursor/skills", GlobalPath: ".cursor/skills", DetectDir: ".cursor", Binary: "cursor"},
	{Name: "opencode", Display: "OpenCode", ProjectPath: ".opencode/skills", GlobalPath: ".config/opencode/skills", DetectDir: ".config/opencode", Binary: "opencode"},
	{Name: "cline", Display: "Cline", ProjectPath: ".cline/skills", GlobalPath: ".cline/skills", DetectDir: ".cline"},
	{Name: "codex", Display: "Codex", ProjectPath: ".codex/skills", GlobalPath: ".codex/skills", DetectDir: ".codex", Binary: "codex"},
	{Name: "windsurf", Display: "Windsurf", ProjectPath: ".windsurf/skills", GlobalPath: ".codeium/windsurf/skills", DetectDir: ".windsurf"},
	{Name: "continue", Display: "Continue", ProjectPath: ".continue/skills", GlobalPath: ".continue/skills", DetectDir: ".continue"},
	{Name: "copilot", Display: "GitHub Copilot", ProjectPath: ".agents/skills", GlobalPath: ".copilot/skills", DetectDir: ".copilot"},
	{Name: "aider", Display: "AiderDesk", ProjectPath: ".aider-desk/skills", GlobalPath: ".aider-desk/skills", DetectDir: ".aider-desk"},
	{Name: "roo", Display: "Roo", ProjectPath: ".roo/skills", GlobalPath: ".roo/skills", DetectDir: ".roo"},
	{Name: "augment", Display: "Augment Code", Aliases: []string{"amplify"}, ProjectPath: ".augment/skills", GlobalPath: ".augment/skills", DetectDir: ".augment"},
	{Name: "zed", Display: "Zed", ProjectPath: ".zed/skills", GlobalPath: ".zed/skills", DetectDir: ".zed", Binary: "zed"},

	// --- Tier 1 additions ---
	{Name: "gemini-cli", Display: "Gemini CLI", ProjectPath: ".agents/skills", GlobalPath: ".gemini/skills", DetectDir: ".gemini", Binary: "gemini"},
	{Name: "goose", Display: "Goose", ProjectPath: ".goose/skills", GlobalPath: ".config/goose/skills", DetectDir: ".config/goose", Binary: "goose"},
	{Name: "kilo", Display: "Kilo Code", ProjectPath: ".kilocode/skills", GlobalPath: ".kilocode/skills", DetectDir: ".kilocode"},
	{Name: "warp", Display: "Warp", ProjectPath: ".agents/skills", GlobalPath: ".agents/skills", DetectDir: ".warp", Binary: "warp"},
	{Name: "amp", Display: "Amp", ProjectPath: ".agents/skills", GlobalPath: ".config/agents/skills", DetectDir: ".config/agents", Binary: "amp"},
	{Name: "qwen-code", Display: "Qwen Code", ProjectPath: ".qwen/skills", GlobalPath: ".qwen/skills", DetectDir: ".qwen"},
	{Name: "crush", Display: "Crush", ProjectPath: ".crush/skills", GlobalPath: ".config/crush/skills", DetectDir: ".config/crush", Binary: "crush"},
	{Name: "openhands", Display: "OpenHands", ProjectPath: ".openhands/skills", GlobalPath: ".openhands/skills", DetectDir: ".openhands"},
	{Name: "kiro-cli", Display: "Kiro CLI", ProjectPath: ".kiro/skills", GlobalPath: ".kiro/skills", DetectDir: ".kiro"},
	{Name: "tabnine-cli", Display: "Tabnine CLI", ProjectPath: ".tabnine/agent/skills", GlobalPath: ".tabnine/agent/skills", DetectDir: ".tabnine"},

	// --- Tier 2/3 additions ---
	{Name: "aider-desk", Display: "AiderDesk", ProjectPath: ".aider-desk/skills", GlobalPath: ".aider-desk/skills", DetectDir: ".aider-desk"},
	{Name: "antigravity", Display: "Antigravity", ProjectPath: ".agents/skills", GlobalPath: ".gemini/antigravity/skills", DetectDir: ".gemini/antigravity"},
	{Name: "bob", Display: "IBM Bob", ProjectPath: ".bob/skills", GlobalPath: ".bob/skills", DetectDir: ".bob"},
	{Name: "openclaw", Display: "OpenClaw", ProjectPath: "skills", GlobalPath: ".openclaw/skills", DetectDir: ".openclaw"},
	{Name: "codearts-agent", Display: "CodeArts Agent", ProjectPath: ".codeartsdoer/skills", GlobalPath: ".codeartsdoer/skills", DetectDir: ".codeartsdoer"},
	{Name: "codebuddy", Display: "CodeBuddy", ProjectPath: ".codebuddy/skills", GlobalPath: ".codebuddy/skills", DetectDir: ".codebuddy"},
	{Name: "codemaker", Display: "Codemaker", ProjectPath: ".codemaker/skills", GlobalPath: ".codemaker/skills", DetectDir: ".codemaker"},
	{Name: "codestudio", Display: "Code Studio", ProjectPath: ".codestudio/skills", GlobalPath: ".codestudio/skills", DetectDir: ".codestudio"},
	{Name: "command-code", Display: "Command Code", ProjectPath: ".commandcode/skills", GlobalPath: ".commandcode/skills", DetectDir: ".commandcode"},
	{Name: "cortex", Display: "Cortex Code", ProjectPath: ".cortex/skills", GlobalPath: ".snowflake/cortex/skills", DetectDir: ".cortex"},
	{Name: "deepagents", Display: "Deep Agents", ProjectPath: ".agents/skills", GlobalPath: ".deepagents/agent/skills", DetectDir: ".deepagents"},
	{Name: "devin", Display: "Devin", ProjectPath: ".devin/skills", GlobalPath: ".config/devin/skills", DetectDir: ".devin"},
	{Name: "dexto", Display: "Dexto", ProjectPath: ".agents/skills", GlobalPath: ".agents/skills", DetectDir: ".dexto"},
	{Name: "droid", Display: "Droid (Factory)", ProjectPath: ".factory/skills", GlobalPath: ".factory/skills", DetectDir: ".factory"},
	{Name: "firebender", Display: "Firebender", ProjectPath: ".agents/skills", GlobalPath: ".firebender/skills", DetectDir: ".firebender"},
	{Name: "forgecode", Display: "ForgeCode", ProjectPath: ".forge/skills", GlobalPath: ".forge/skills", DetectDir: ".forge"},
	{Name: "hermes-agent", Display: "Hermes Agent", ProjectPath: ".hermes/skills", GlobalPath: ".hermes/skills", DetectDir: ".hermes"},
	{Name: "iflow-cli", Display: "iFlow CLI", ProjectPath: ".iflow/skills", GlobalPath: ".iflow/skills", DetectDir: ".iflow"},
	{Name: "junie", Display: "Junie", ProjectPath: ".junie/skills", GlobalPath: ".junie/skills", DetectDir: ".junie"},
	{Name: "kimi-cli", Display: "Kimi Code CLI", ProjectPath: ".agents/skills", GlobalPath: ".config/agents/skills", DetectDir: ".kimi"},
	{Name: "kode", Display: "Kode", ProjectPath: ".kode/skills", GlobalPath: ".kode/skills", DetectDir: ".kode"},
	{Name: "mcpjam", Display: "MCPJam", ProjectPath: ".mcpjam/skills", GlobalPath: ".mcpjam/skills", DetectDir: ".mcpjam"},
	{Name: "mistral-vibe", Display: "Mistral Vibe", ProjectPath: ".vibe/skills", GlobalPath: ".vibe/skills", DetectDir: ".vibe"},
	{Name: "mux", Display: "Mux", ProjectPath: ".mux/skills", GlobalPath: ".mux/skills", DetectDir: ".mux"},
	{Name: "neovate", Display: "Neovate", ProjectPath: ".neovate/skills", GlobalPath: ".neovate/skills", DetectDir: ".neovate"},
	{Name: "pi", Display: "Pi", ProjectPath: ".pi/skills", GlobalPath: ".pi/agent/skills", DetectDir: ".pi"},
	{Name: "pochi", Display: "Pochi", ProjectPath: ".pochi/skills", GlobalPath: ".pochi/skills", DetectDir: ".pochi"},
	{Name: "qoder", Display: "Qoder", ProjectPath: ".qoder/skills", GlobalPath: ".qoder/skills", DetectDir: ".qoder"},
	{Name: "replit", Display: "Replit", ProjectPath: ".agents/skills", GlobalPath: ".config/agents/skills", DetectDir: ".replit"},
	{Name: "rovodev", Display: "Rovo Dev", ProjectPath: ".rovodev/skills", GlobalPath: ".rovodev/skills", DetectDir: ".rovodev"},
	{Name: "trae", Display: "Trae", ProjectPath: ".trae/skills", GlobalPath: ".trae/skills", DetectDir: ".trae"},
	{Name: "trae-cn", Display: "Trae CN", ProjectPath: ".trae/skills", GlobalPath: ".trae-cn/skills", DetectDir: ".trae-cn"},
	{Name: "zencoder", Display: "Zencoder", ProjectPath: ".zencoder/skills", GlobalPath: ".zencoder/skills", DetectDir: ".zencoder"},
	{Name: "adal", Display: "AdaL", ProjectPath: ".adal/skills", GlobalPath: ".adal/skills", DetectDir: ".adal"},

	// Target-only: never auto-detected, must be requested with -a universal.
	{Name: "universal", Display: "Universal", ProjectPath: ".agents/skills", GlobalPath: ".config/agents/skills", DetectDir: ""},
}

// FindDef looks up an agent by canonical name or alias. The returned alias
// string is non-empty when the lookup matched via an alias, so callers can warn
// about the deprecated name.
func FindDef(name string) (def Def, alias string, found bool) {
	for _, d := range Defs {
		if d.Name == name {
			return d, "", true
		}
	}
	for _, d := range Defs {
		for _, a := range d.Aliases {
			if a == name {
				return d, name, true
			}
		}
	}
	return Def{}, "", false
}

// InstallPath returns where skills are installed for def. For global installs
// the path is absolute under $HOME; for local installs it is the relative
// ProjectPath.
func InstallPath(def Def, global bool) (string, error) {
	if !global {
		return def.ProjectPath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, def.GlobalPath), nil
}

// Detect returns every agent whose DetectDir exists under $HOME.
func Detect(global bool) ([]Agent, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	var out []Agent
	for _, d := range Defs {
		if d.DetectDir == "" {
			continue
		}
		if fi, err := os.Stat(filepath.Join(home, d.DetectDir)); err == nil && fi.IsDir() {
			ip, err := InstallPath(d, global)
			if err != nil {
				return nil, err
			}
			out = append(out, Agent{Def: d, InstallPath: ip, Detected: true})
		}
	}
	return out, nil
}

// Resolve builds agents from explicit names (as passed via -a). Unknown names
// return an error. Aliased names resolve to their canonical Def; the alias used
// is reported via aliasesUsed so the caller can warn.
func Resolve(names []string, global bool) (out []Agent, aliasesUsed map[string]string, err error) {
	aliasesUsed = map[string]string{}
	for _, name := range names {
		def, alias, found := FindDef(name)
		if !found {
			return nil, nil, fmt.Errorf("unknown agent: %q", name)
		}
		if alias != "" {
			aliasesUsed[alias] = def.Name
		}
		ip, err := InstallPath(def, global)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, Agent{Def: def, InstallPath: ip, Detected: true})
	}
	return out, aliasesUsed, nil
}
