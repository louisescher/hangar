package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/security/audit"
	"github.com/louisescher/hangar/internal/spec"
)

// Update re-resolves installed skills to their latest ref (or re-pins explicit
// refs), reinstalls those that changed, and flags rewritten tags. With name set,
// only that skill is updated. A single shared audit log accumulates all changes.
func (e *Engine) Update(ctx context.Context, name string, opt InstallOptions) (install.Report, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return install.Report{}, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return install.Report{}, err
	}

	var targets []lockfile.Entry
	for _, e := range lf.Skills {
		if name == "" || e.Name == name {
			targets = append(targets, e)
		}
	}
	if name != "" && len(targets) == 0 {
		return install.Report{}, fmt.Errorf("no installed skill named %q", name)
	}

	agts, err := resolveAgents(opt.Agents, opt.Global)
	if err != nil {
		return install.Report{}, err
	}

	log := audit.New(audit.OpUpdate)
	rep := install.Report{Audit: log}

	for _, entry := range targets {
		s, err := specFromEntry(entry)
		if err != nil {
			// Unsupported source kind for update; leave it in place.
			continue
		}

		d, err := e.Discover(ctx, s)
		if err != nil {
			return rep, fmt.Errorf("update %q: %w", entry.Name, err)
		}

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
			d.Close()
			continue
		}

		req := install.Request{
			BaseDir:   baseDir,
			Agents:    agts,
			Options:   install.Options{Global: opt.Global, Security: opt.Security},
			Operation: audit.OpUpdate,
			Audit:     log,
			Meta:      buildMeta(s, d),
		}
		if entry.Kind == lockfile.KindRef {
			req.References = buildReferences(s, d.References)
		} else {
			req.Skills = pickByName(d.Skills, entry.Name)
		}

		sub, _ := install.Install(req)
		rep.Skills = append(rep.Skills, sub.Skills...)
		if sub.InstalledInstruction != "" {
			rep.InstalledInstruction = sub.InstalledInstruction
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
