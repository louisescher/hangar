package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/spec"
)

// InstallOptions configures a headless install via the engine.
type InstallOptions struct {
	Agents  []string // explicit agent names; empty => auto-detect
	Global  bool
	All     bool   // install every discovered skill
	Yes     bool   // reserved for confirmation skipping
	BaseDir string // install root; default cwd (or $HOME when Global)
}

// Install discovers a source, selects skills (all, the single match, or the
// #skill filter), resolves target agents, and installs.
func (e *Engine) Install(ctx context.Context, s spec.SourceSpec, opt InstallOptions) (install.Report, error) {
	d, err := e.Discover(ctx, s)
	if err != nil {
		return install.Report{}, err
	}
	defer d.Close()

	selected, err := selectSkills(d.Skills, opt)
	if err != nil {
		return install.Report{}, err
	}

	agts, err := resolveAgents(opt.Agents, opt.Global)
	if err != nil {
		return install.Report{}, err
	}

	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return install.Report{}, err
	}

	return install.Install(install.Request{
		BaseDir: baseDir,
		Skills:  selected,
		Agents:  agts,
		Options: install.Options{Global: opt.Global},
		Meta: install.SourceMeta{
			Source:    lockSource(s),
			Subpath:   s.Subpath,
			Ref:       d.Ref,
			SHA:       d.SHA,
			IsTag:     d.IsTag,
			Pinned:    s.Pinned,
			CrawlRoot: d.Root,
		},
	})
}

// Remove deletes an installed skill (or reference) by name from every target
// agent, the canonical store, and the lockfile.
func (e *Engine) Remove(name string, opt InstallOptions) error {
	agts, err := resolveAgents(opt.Agents, opt.Global)
	if err != nil {
		return err
	}
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return err
	}
	removed, err := install.Remove(baseDir, name, agts, opt.Global)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("no installed skill named %q", name)
	}
	return nil
}

func selectSkills(all []Skill, opt InstallOptions) ([]Skill, error) {
	switch {
	case len(all) == 0:
		return nil, fmt.Errorf("no skills found")
	case opt.All, len(all) == 1:
		return all, nil
	default:
		return nil, fmt.Errorf("source has %d skills; pass --all to install all, or select one with #name (interactive picker coming soon)", len(all))
	}
}

func resolveAgents(names []string, global bool) ([]agents.Agent, error) {
	if len(names) > 0 {
		agts, _, err := agents.Resolve(names, global)
		return agts, err
	}
	return agents.Detect(global)
}

func resolveBaseDir(opt InstallOptions) (string, error) {
	if opt.BaseDir != "" {
		return opt.BaseDir, nil
	}
	if opt.Global {
		return os.UserHomeDir()
	}
	return os.Getwd()
}

// lockSource renders the lockfile "source" string for a spec.
func lockSource(s spec.SourceSpec) string {
	switch s.Kind {
	case spec.KindGitHub:
		return s.Owner + "/" + s.Repo
	case spec.KindLocal:
		return "file://" + s.Path
	case spec.KindNPM:
		if s.File != "" {
			return "npm:" + s.Pkg + "#" + s.File
		}
		return "npm:" + s.Pkg
	default:
		return s.Raw
	}
}
