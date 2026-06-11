// Package engine is the facade the CLI and TUI call. It dispatches a source
// spec to the right fetch backend, runs discovery, and (in later milestones)
// drives installation.
package engine

import (
	"context"
	"fmt"

	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/fetch/github"
	"github.com/louisescher/hangar/internal/fetch/local"
	"github.com/louisescher/hangar/internal/fetch/npmreg"
	"github.com/louisescher/hangar/internal/spec"
)

// Skill is the discovered-skill type surfaced to callers.
type Skill = discover.Skill

// Engine holds the configured fetch backends.
type Engine struct {
	gh    fetch.Fetcher
	npm   fetch.Fetcher
	local fetch.Fetcher
}

// New constructs an Engine with default backends (GitHub authenticated from the
// ambient token, npm from the resolved .npmrc config, local filesystem).
func New() *Engine {
	return &Engine{
		gh:    github.New(nil, config.GitHubToken()),
		npm:   npmreg.New(nil, config.LoadNPMRC()),
		local: local.New(),
	}
}

// Discovered is the result of crawling a source. Close must be called to release
// any temporary extraction directory; until then the skills' AbsPath files
// remain available for installation.
type Discovered struct {
	Skills     []Skill
	References []discover.RefDoc // npm doc references (README, docs/**); empty otherwise
	Source     string
	Root       string // on-disk crawl root (for vendoring); valid until Close
	Ref        string
	SHA        string
	IsTag      bool
	cleanup    func() error
}

// Close releases temporary resources held by the discovery.
func (d *Discovered) Close() error {
	if d == nil || d.cleanup == nil {
		return nil
	}
	return d.cleanup()
}

func (e *Engine) fetcherFor(s spec.SourceSpec) (fetch.Fetcher, error) {
	switch s.Kind {
	case spec.KindGitHub:
		return e.gh, nil
	case spec.KindNPM:
		return e.npm, nil
	case spec.KindLocal:
		return e.local, nil
	default:
		return nil, fmt.Errorf("unsupported source kind")
	}
}

// Discover fetches the source and crawls it for skills (and, for npm sources,
// reference docs). For GitHub/local, spec.Skill filters the skills; for npm,
// spec.File selects a single reference doc. The caller owns the returned
// Discovered and must Close it.
func (e *Engine) Discover(ctx context.Context, s spec.SourceSpec) (*Discovered, error) {
	f, err := e.fetcherFor(s)
	if err != nil {
		return nil, err
	}
	res, err := f.Fetch(ctx, s)
	if err != nil {
		return nil, err
	}

	skills, refs, err := e.crawl(s, res.Root)
	if err != nil {
		_ = res.Cleanup()
		return nil, err
	}

	return &Discovered{
		Skills:     skills,
		References: refs,
		Source:     sourceLabel(s),
		Root:       res.Root,
		Ref:        res.Ref,
		SHA:        res.SHA,
		IsTag:      res.IsTag,
		cleanup:    res.Cleanup,
	}, nil
}

// crawl finds skills and reference docs under root according to the spec kind.
func (e *Engine) crawl(s spec.SourceSpec, root string) ([]Skill, []discover.RefDoc, error) {
	if s.Kind == spec.KindNPM {
		// "#file" selects exactly one reference doc (no skills).
		if s.File != "" {
			refs, err := discover.CollectRefDocs(root, []string{s.File})
			if err != nil {
				return nil, nil, fmt.Errorf("reference %q in %s: %w", s.File, sourceLabel(s), err)
			}
			if len(refs) == 0 {
				return nil, nil, fmt.Errorf("no reference %q found in %s", s.File, sourceLabel(s))
			}
			return nil, refs, nil
		}
		skills, err := discover.DiscoverSkills(root)
		if err != nil {
			return nil, nil, err
		}
		refs, err := discover.CollectRefDocs(root, nil)
		if err != nil {
			return nil, nil, err
		}
		if len(skills) == 0 && len(refs) == 0 {
			return nil, nil, fmt.Errorf("no skills or references found in %s", sourceLabel(s))
		}
		return skills, refs, nil
	}

	skills, err := discover.DiscoverSkills(root)
	if err != nil {
		return nil, nil, err
	}
	if s.Skill != "" {
		skills = filterByName(skills, s.Skill)
		if len(skills) == 0 {
			return nil, nil, fmt.Errorf("no skill named %q found in %s", s.Skill, sourceLabel(s))
		}
	}
	return skills, nil, nil
}

func filterByName(skills []Skill, name string) []Skill {
	var out []Skill
	for _, s := range skills {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func sourceLabel(s spec.SourceSpec) string {
	switch s.Kind {
	case spec.KindGitHub:
		return s.Owner + "/" + s.Repo
	case spec.KindNPM:
		return "npm:" + s.Pkg
	default:
		return s.Path
	}
}
