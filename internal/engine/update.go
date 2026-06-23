package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/security/audit"
	"github.com/louisescher/hangar/internal/spec"
)

// Update re-resolves installed skills to their latest ref (or re-pins explicit
// refs), reinstalls those that changed, and flags rewritten tags. With name set,
// only that skill is updated. A single shared audit log accumulates all changes.
func (e *Engine) Update(ctx context.Context, name string, opt InstallOptions) (install.Report, error) {
	baseDir, lf, err := e.loadForUpdate(opt)
	if err != nil {
		return install.Report{}, err
	}

	var targets []lockfile.Entry
	for _, en := range lf.Skills {
		if name == "" || en.Name == name {
			targets = append(targets, en)
		}
	}
	if name != "" && len(targets) == 0 {
		return install.Report{}, fmt.Errorf("no installed skill named %q", name)
	}
	return e.updateTargets(ctx, baseDir, targets, opt)
}

// UpdateNames updates exactly the named entries — used by the manage screen's
// "update all outdated", which already knows (from the cached status) which
// entries are stale, so it can skip re-resolving the rest.
func (e *Engine) UpdateNames(ctx context.Context, names []string, opt InstallOptions) (install.Report, error) {
	baseDir, lf, err := e.loadForUpdate(opt)
	if err != nil {
		return install.Report{}, err
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var targets []lockfile.Entry
	for _, en := range lf.Skills {
		if want[en.Name] {
			targets = append(targets, en)
		}
	}
	return e.updateTargets(ctx, baseDir, targets, opt)
}

func (e *Engine) loadForUpdate(opt InstallOptions) (string, *lockfile.Lockfile, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return "", nil, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return "", nil, err
	}
	return baseDir, lf, nil
}

// updateTargets re-resolves and reinstalls the given entries, grouping them by
// source so each source is fetched once. Unchanged entries are skipped; pinned
// tags whose SHA moved are flagged.
func (e *Engine) updateTargets(ctx context.Context, baseDir string, targets []lockfile.Entry, opt InstallOptions) (install.Report, error) {
	agts, err := resolveAgents(opt.Agents, opt.Global)
	if err != nil {
		return install.Report{}, err
	}

	log := audit.New(audit.OpUpdate)
	rep := install.Report{Audit: log}

	for _, g := range e.groupEntries(targets) {
		s := g.spec
		if opt.OnProgress != nil {
			opt.OnProgress(install.Event{Phase: "fetching", Name: sourceLabel(s)})
		}
		d, err := e.Discover(ctx, s)
		if err != nil {
			// Source deleted upstream: keep its skills, report them, carry on.
			if errors.Is(err, fetch.ErrNotFound) {
				for _, entry := range g.entries {
					rep.Gone = append(rep.Gone, entry.Name)
				}
				continue
			}
			return rep, fmt.Errorf("update from %s: %w", sourceLabel(s), err)
		}

		for _, entry := range g.entries {
			// Tag-rewrite detection: a pinned tag whose SHA moved underneath us.
			if s.Kind.IsGitTarball() && entry.Pinned && d.IsTag &&
				d.Ref == entry.Ref && entry.SHA != "" && d.SHA != entry.SHA {
				log.AddFinding(audit.TagRewriteFinding(entry.Name, entry.Ref, entry.SHA, d.SHA))
			}

			// Already up to date. For npm the version lives in entry.Version; for
			// GitHub it is the ref/SHA pair.
			current := entry.Ref
			if s.Kind == spec.KindNPM {
				current = entry.Version
			}
			if d.Ref == current && d.SHA == entry.SHA {
				continue
			}

			if opt.OnProgress != nil {
				opt.OnProgress(install.Event{Phase: "updating", Name: entry.Name})
			}
			skills, refs, meta, cleanup, err := e.resolveEntry(ctx, g, d, entry)
			if err != nil {
				cleanup()
				// Skill removed/renamed upstream: keep it for manual removal.
				if errors.Is(err, fetch.ErrNotFound) {
					rep.Gone = append(rep.Gone, entry.Name)
					continue
				}
				d.Close()
				return rep, fmt.Errorf("update %q: %w", entry.Name, err)
			}
			sub, _ := install.Install(install.Request{
				BaseDir:    baseDir,
				Skills:     skills,
				References: buildReferences(s, refs),
				Agents:     agts,
				Options:    install.Options{Global: opt.Global, Security: opt.Security},
				Operation:  audit.OpUpdate,
				Audit:      log,
				Meta:       meta,
			})
			cleanup()
			rep.Skills = append(rep.Skills, sub.Skills...)
			if sub.InstalledInstruction != "" {
				rep.InstalledInstruction = sub.InstalledInstruction
			}
		}
		d.Close()
	}

	return rep, nil
}

// specFromEntry reconstructs a source spec from a lockfile entry so it can be
// re-resolved and re-fetched. Auto (unpinned) entries drop the ref to pick up
// the latest; pinned entries re-resolve their exact ref. Self-hosted forge
// hosts are resolved via the engine's configured registry.
func (e *Engine) specFromEntry(entry lockfile.Entry) (spec.SourceSpec, error) {
	switch {
	case strings.HasPrefix(entry.Source, "npm:"):
		s := spec.SourceSpec{
			Kind:    spec.KindNPM,
			Pkg:     strings.TrimPrefix(entry.Source, "npm:"),
			Subpath: entry.Subpath,
			File:    entry.File, // set for reference entries; "" for skills
			Pinned:  entry.Pinned,
		}
		if entry.Pinned {
			s.Ref = entry.Version // re-resolve the pinned exact version
		}
		return s, nil
	case strings.HasPrefix(entry.Source, "file://"):
		return spec.SourceSpec{
			Kind:  spec.KindLocal,
			Path:  strings.TrimPrefix(entry.Source, "file://"),
			Skill: entry.Name,
		}, nil
	case strings.HasPrefix(entry.Source, "http://"), strings.HasPrefix(entry.Source, "https://"):
		// A non-GitHub forge: source is the canonical "https://host/owner/repo".
		s, err := specFromForgeURL(entry.Source, e.hostForges)
		if err != nil {
			return spec.SourceSpec{}, err
		}
		s.Subpath = entry.Subpath
		s.Pinned = entry.Pinned
		if entry.Pinned {
			s.Ref = entry.Ref
		}
		return s, nil
	default:
		owner, repo, ok := strings.Cut(entry.Source, "/")
		if !ok {
			return spec.SourceSpec{}, fmt.Errorf("malformed github source %q", entry.Source)
		}
		s := spec.SourceSpec{
			Kind:    spec.KindGitHub,
			Owner:   owner,
			Repo:    repo,
			Subpath: entry.Subpath,
			Pinned:  entry.Pinned,
		}
		if entry.Pinned {
			s.Ref = entry.Ref
		}
		return s, nil
	}
}

// specFromForgeURL reconstructs a KindGit spec's host/owner/repo/forge from a
// canonical "https://host/owner/repo" lockfile source. A host not present in
// the registry falls back to ForgeGeneric so committed lockfiles still resolve
// public repos on a machine without that host configured.
func specFromForgeURL(source string, hosts map[string]spec.Forge) (spec.SourceSpec, error) {
	rest := strings.TrimPrefix(strings.TrimPrefix(source, "https://"), "http://")
	host, ownerRepo, ok := strings.Cut(rest, "/")
	if !ok {
		return spec.SourceSpec{}, fmt.Errorf("malformed git source %q", source)
	}
	owner, repo, ok := strings.Cut(ownerRepo, "/")
	if !ok || owner == "" || repo == "" {
		return spec.SourceSpec{}, fmt.Errorf("malformed git source %q", source)
	}
	forge, ok := spec.ForgeForHost(host, hosts)
	if !ok {
		forge = spec.ForgeGeneric
	}
	return spec.SourceSpec{
		Kind:  spec.KindGit,
		Forge: forge,
		Host:  "https://" + host,
		Owner: owner,
		Repo:  repo,
	}, nil
}

func pickByName(skills []Skill, name string) []Skill {
	for _, s := range skills {
		if s.Name == name {
			return []Skill{s}
		}
	}
	return skills // fall back to all (subpath already isolated the skill)
}
