package engine

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/security/sanitize"
	"github.com/louisescher/hangar/internal/spec"
)

// InstallOptions configures a headless install via the engine.
type InstallOptions struct {
	Agents   []string // explicit agent names; empty => auto-detect
	Global   bool
	All      bool   // install every discovered skill
	Yes      bool   // reserved for confirmation skipping
	BaseDir  string // install root; default cwd (or $HOME when Global)
	Security sanitize.Opts
}

// Install discovers a source, selects skills (all, the single match, or the
// #skill filter), resolves target agents, and installs.
func (e *Engine) Install(ctx context.Context, s spec.SourceSpec, opt InstallOptions) (install.Report, error) {
	d, err := e.Discover(ctx, s)
	if err != nil {
		return install.Report{}, err
	}
	defer d.Close()

	selected, err := selectSkills(d.Skills, d.References, opt)
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
		BaseDir:    baseDir,
		Skills:     selected,
		References: buildReferences(s, d.References),
		Agents:     agts,
		Options:    install.Options{Global: opt.Global, Security: opt.Security},
		Meta:       buildMeta(s, d),
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

func selectSkills(all []Skill, refs []discover.RefDoc, opt InstallOptions) ([]Skill, error) {
	switch {
	case len(all) == 0:
		// A references-only source (e.g. an npm package with docs but no
		// SKILL.md) is a valid install: the references ride along on their own.
		if len(refs) > 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("no skills found")
	case opt.All, len(all) == 1:
		return all, nil
	default:
		return nil, fmt.Errorf("source has %d skills; pass --all to install all, or select one with #name (interactive picker coming soon)", len(all))
	}
}

// buildReferences converts discovered reference docs into install.Reference
// values, deriving a stable, unique name from the package and the doc path.
func buildReferences(s spec.SourceSpec, docs []discover.RefDoc) []install.Reference {
	if len(docs) == 0 {
		return nil
	}
	out := make([]install.Reference, 0, len(docs))
	for _, d := range docs {
		out = append(out, install.Reference{
			Name:    referenceName(s.Pkg, d.RelPath),
			AbsPath: d.AbsPath,
			File:    d.RelPath,
		})
	}
	return out
}

// referenceName derives a reference's lockfile key from its package and
// repo-relative path: the package base name for a README, or
// "<pkgbase>-<doc-slug>" for a doc under docs/.
func referenceName(pkg, relPath string) string {
	base := pkg
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:] // @scope/pkg -> pkg
	}
	base = strings.TrimPrefix(base, "@")
	if base == "" {
		base = "package"
	}

	slug := strings.TrimSuffix(relPath, path.Ext(relPath))
	slug = strings.TrimPrefix(slug, "docs/")
	if strings.EqualFold(slug, "readme") {
		return base
	}
	slug = strings.ToLower(strings.NewReplacer("/", "-", " ", "-").Replace(slug))
	if slug == "" {
		return base
	}
	return base + "-" + slug
}

// buildMeta assembles the lockfile/source metadata for an install, routing the
// resolved ref into Version for npm and into Ref/SHA for GitHub.
func buildMeta(s spec.SourceSpec, d *Discovered) install.SourceMeta {
	m := install.SourceMeta{
		Source:    lockSource(s),
		Subpath:   s.Subpath,
		Pinned:    s.Pinned,
		CrawlRoot: d.Root,
	}
	if s.Kind == spec.KindNPM {
		m.Version = d.Ref
	} else {
		m.Ref = d.Ref
		m.SHA = d.SHA
		m.IsTag = d.IsTag
	}
	return m
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
		// The selected #file (if any) is recorded per reference entry, so the
		// source string stays the bare package.
		return "npm:" + s.Pkg
	default:
		return s.Raw
	}
}
