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
	"github.com/louisescher/hangar/internal/spec"
)

// Skill is the discovered-skill type surfaced to callers.
type Skill = discover.Skill

// Engine holds the configured fetch backends.
type Engine struct {
	gh    fetch.Fetcher
	local fetch.Fetcher
}

// New constructs an Engine with default backends (GitHub authenticated from the
// ambient token, local filesystem).
func New() *Engine {
	return &Engine{
		gh:    github.New(nil, config.GitHubToken()),
		local: local.New(),
	}
}

// Discovered is the result of crawling a source. Close must be called to release
// any temporary extraction directory; until then the skills' AbsPath files
// remain available for installation.
type Discovered struct {
	Skills  []Skill
	Source  string
	Root    string // on-disk crawl root (for vendoring); valid until Close
	Ref     string
	SHA     string
	IsTag   bool
	cleanup func() error
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
	case spec.KindLocal:
		return e.local, nil
	case spec.KindNPM:
		return nil, fmt.Errorf("npm sources are not yet supported")
	default:
		return nil, fmt.Errorf("unsupported source kind")
	}
}

// Discover fetches the source and crawls it for skills, filtering by spec.Skill
// when set. The caller owns the returned Discovered and must Close it.
func (e *Engine) Discover(ctx context.Context, s spec.SourceSpec) (*Discovered, error) {
	f, err := e.fetcherFor(s)
	if err != nil {
		return nil, err
	}
	res, err := f.Fetch(ctx, s)
	if err != nil {
		return nil, err
	}
	skills, err := discover.DiscoverSkills(res.Root)
	if err != nil {
		_ = res.Cleanup()
		return nil, err
	}
	if s.Skill != "" {
		skills = filterByName(skills, s.Skill)
		if len(skills) == 0 {
			_ = res.Cleanup()
			return nil, fmt.Errorf("no skill named %q found in %s", s.Skill, sourceLabel(s))
		}
	}
	return &Discovered{
		Skills:  skills,
		Source:  sourceLabel(s),
		Root:    res.Root,
		Ref:     res.Ref,
		SHA:     res.SHA,
		IsTag:   res.IsTag,
		cleanup: res.Cleanup,
	}, nil
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
