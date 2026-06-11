// Package install is the top-level integrator: it copies selected skills into
// the canonical .agents/skills store, vendors their in-repo dependencies, links
// or copies them into each target agent, updates the lockfile, and rebuilds the
// references block.
package install

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/fsutil"
	"github.com/louisescher/hangar/internal/link"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/refsblock"
)

// vendorDepth bounds how deep ResolveSkillFiles follows markdown links.
const vendorDepth = 3

// Options controls install behavior.
type Options struct {
	Global bool
}

// SourceMeta describes the source the skills came from, for lockfile and
// vendoring purposes.
type SourceMeta struct {
	Source    string // lockfile "source": owner/repo, file://path, npm:pkg
	Subpath   string // spec subpath (crawl root within the source)
	Ref       string
	SHA       string
	IsTag     bool
	Pinned    bool
	CrawlRoot string // on-disk crawl root, used to bound vendoring
}

// Request bundles everything Install needs.
type Request struct {
	BaseDir string
	Skills  []discover.Skill
	Meta    SourceMeta
	Agents  []agents.Agent
	Options Options
	Now     time.Time
}

// SkillResult reports the outcome for one skill.
type SkillResult struct {
	Name            string
	Kind            string
	InstalledAgents []string
	FailedAgents    map[string]string // agent name -> failure reason
}

// Report summarizes an install run.
type Report struct {
	Skills               []SkillResult
	InstalledAgents      []string // union across skills
	FailedAgents         []string // union across skills
	InstalledInstruction string   // instruction file updated for references, if any
}

// Install performs the pipeline for the given request.
func Install(req Request) (Report, error) {
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	lf, err := lockfile.Load(req.BaseDir)
	if err != nil {
		return Report{}, err
	}

	canonicalRoot := filepath.Join(req.BaseDir, ".agents", "skills")
	linkBase := req.BaseDir
	if req.Options.Global {
		linkBase = "" // agent.InstallPath is already absolute for global installs
	}

	var rep Report
	installedSet := map[string]bool{}
	failedSet := map[string]bool{}

	for _, sk := range req.Skills {
		canonicalDir := filepath.Join(canonicalRoot, sk.Name)
		if err := link.Copy(sk.AbsPath, canonicalDir); err != nil {
			return rep, fmt.Errorf("copy skill %q: %w", sk.Name, err)
		}
		if err := vendorDeps(req.Meta.CrawlRoot, sk.AbsPath, canonicalDir, canonicalRoot); err != nil {
			return rep, fmt.Errorf("vendor files for %q: %w", sk.Name, err)
		}

		sr := SkillResult{Name: sk.Name, Kind: lockfile.KindSkill, FailedAgents: map[string]string{}}
		for _, ag := range req.Agents {
			agentSkillsDir := filepath.Join(linkBase, ag.InstallPath)
			// When an agent installs into the canonical store itself, the skill
			// is already present — no link or copy needed.
			if filepath.Clean(agentSkillsDir) == filepath.Clean(canonicalRoot) {
				sr.InstalledAgents = append(sr.InstalledAgents, ag.Def.Name)
				installedSet[ag.Def.Name] = true
				continue
			}
			target := filepath.Join(agentSkillsDir, sk.Name)

			var linkErr error
			if req.Options.Global {
				linkErr = link.Copy(canonicalDir, target)
			} else {
				linkErr = link.Symlink(canonicalDir, target)
			}
			if linkErr != nil {
				sr.FailedAgents[ag.Def.Name] = linkErr.Error()
				failedSet[ag.Def.Name] = true
				continue
			}
			sr.InstalledAgents = append(sr.InstalledAgents, ag.Def.Name)
			installedSet[ag.Def.Name] = true
		}

		lf.Upsert(lockfile.Entry{
			Name:        sk.Name,
			Source:      req.Meta.Source,
			Subpath:     path.Join(req.Meta.Subpath, sk.RelPath),
			Ref:         req.Meta.Ref,
			SHA:         req.Meta.SHA,
			InstalledAt: req.Now,
			Pinned:      req.Meta.Pinned,
			Kind:        lockfile.KindSkill,
		})
		rep.Skills = append(rep.Skills, sr)
	}

	if err := lf.Save(); err != nil {
		return rep, fmt.Errorf("save lockfile: %w", err)
	}

	instruction, err := rebuildReferences(req.BaseDir, lf)
	if err != nil {
		return rep, err
	}
	rep.InstalledInstruction = instruction

	rep.InstalledAgents = keys(installedSet)
	rep.FailedAgents = keys(failedSet)
	return rep, nil
}

// vendorDeps copies in-repo files referenced by the skill (but living outside
// its directory) into the canonical store, preserving their path relative to
// the skill so existing relative links keep resolving. Destinations are
// constrained to the canonical skills root.
func vendorDeps(crawlRoot, skillDir, canonicalDir, canonicalRoot string) error {
	if crawlRoot == "" {
		return nil
	}
	extra, err := discover.ResolveSkillFiles(crawlRoot, skillDir, vendorDepth)
	if err != nil {
		return err
	}
	for _, repoRel := range extra {
		src := filepath.Join(crawlRoot, filepath.FromSlash(repoRel))
		skillRel, err := filepath.Rel(skillDir, src)
		if err != nil {
			continue
		}
		dst := filepath.Join(canonicalDir, skillRel)
		if err := fsutil.MustWithinRoot(canonicalRoot, dst); err != nil {
			// Reference would land outside the skills store; skip it rather than escape.
			continue
		}
		if err := fsutil.CopyFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// rebuildReferences regenerates the references block from the lockfile's ref
// entries.
func rebuildReferences(baseDir string, lf *lockfile.Lockfile) (string, error) {
	refs := lf.Refs()
	links := make([]refsblock.Link, 0, len(refs))
	for _, e := range refs {
		refPath := filepath.Join(baseDir, ".agents", "references", e.Name, "REFERENCE.md")
		title := e.Name
		if data, err := os.ReadFile(refPath); err == nil {
			title = refsblock.ExtractTitle(data, e.Name)
		}
		links = append(links, refsblock.Link{
			Title: title,
			Path:  "./" + filepath.ToSlash(filepath.Join(".agents", "references", e.Name, "REFERENCE.md")),
		})
	}
	return refsblock.Rebuild(baseDir, links)
}

func keys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
