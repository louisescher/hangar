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

	for _, g := range groupEntries(targets) {
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
			if s.Kind == spec.KindGitHub && entry.Pinned && d.IsTag &&
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
// the latest; pinned entries re-resolve their exact ref.
func specFromEntry(e lockfile.Entry) (spec.SourceSpec, error) {
	switch {
	case strings.HasPrefix(e.Source, "npm:"):
		s := spec.SourceSpec{
			Kind:    spec.KindNPM,
			Pkg:     strings.TrimPrefix(e.Source, "npm:"),
			Subpath: e.Subpath,
			File:    e.File, // set for reference entries; "" for skills
			Pinned:  e.Pinned,
		}
		if e.Pinned {
			s.Ref = e.Version // re-resolve the pinned exact version
		}
		return s, nil
	case strings.HasPrefix(e.Source, "file://"):
		return spec.SourceSpec{
			Kind:  spec.KindLocal,
			Path:  strings.TrimPrefix(e.Source, "file://"),
			Skill: e.Name,
		}, nil
	default:
		owner, repo, ok := strings.Cut(e.Source, "/")
		if !ok {
			return spec.SourceSpec{}, fmt.Errorf("malformed github source %q", e.Source)
		}
		s := spec.SourceSpec{
			Kind:    spec.KindGitHub,
			Owner:   owner,
			Repo:    repo,
			Subpath: e.Subpath,
			Pinned:  e.Pinned,
		}
		if e.Pinned {
			s.Ref = e.Ref
		}
		return s, nil
	}
}

func pickByName(skills []Skill, name string) []Skill {
	for _, s := range skills {
		if s.Name == name {
			return []Skill{s}
		}
	}
	return skills // fall back to all (subpath already isolated the skill)
}
