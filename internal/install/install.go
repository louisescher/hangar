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
	"github.com/louisescher/hangar/internal/security/audit"
	"github.com/louisescher/hangar/internal/security/sanitize"
)

// skillManifest is the file used to represent a skill's content in the audit log.
const skillManifest = "SKILL.md"

// vendorDepth bounds how deep ResolveSkillFiles follows markdown links.
const vendorDepth = 3

// Options controls install behavior.
type Options struct {
	Global   bool
	Security sanitize.Opts
}

// SourceMeta describes the source the skills came from, for lockfile and
// vendoring purposes.
type SourceMeta struct {
	Source    string // lockfile "source": owner/repo, file://path, npm:pkg
	Subpath   string // spec subpath (crawl root within the source)
	Ref       string // branch/tag (GitHub)
	SHA       string // resolved commit SHA (GitHub)
	Version   string // package version (npm)
	IsTag     bool
	Pinned    bool
	CrawlRoot string // on-disk crawl root, used to bound vendoring
}

// Reference is a documentation file installed under
// .agents/references/<Name>/REFERENCE.md and surfaced via the instructions
// block (it is never symlinked into agents).
type Reference struct {
	Name    string // unique lockfile key and directory name
	AbsPath string // source markdown file on disk
	File    string // source-relative path, recorded in the lockfile
}

// Event reports install progress. Phase is one of "skill", "reference", or
// "finalize"; Index/Total count items (skills + references).
type Event struct {
	Phase string
	Name  string
	Index int
	Total int
}

// Request bundles everything Install needs.
type Request struct {
	BaseDir    string
	Skills     []discover.Skill
	References []Reference // npm doc references (README, docs/**)
	Meta       SourceMeta
	Agents     []agents.Agent
	Options    Options
	Operation  audit.Operation // OpInstall (default) or OpUpdate
	Audit      *audit.Log      // optional shared audit log; created if nil
	OnProgress func(Event)     // optional progress callback (called synchronously)
	Now        time.Time
}

func (req Request) emit(ev Event) {
	if req.OnProgress != nil {
		req.OnProgress(ev)
	}
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
	Gone                 []string // entries skipped because their source/skill no longer exists upstream (kept on disk)
	Audit                *audit.Log
}

// Install performs the pipeline for the given request.
func Install(req Request) (Report, error) {
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	op := req.Operation
	if op == "" {
		op = audit.OpInstall
	}
	if req.Audit == nil {
		req.Audit = audit.New(op)
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
	total := len(req.Skills) + len(req.References)
	idx := 0

	for _, sk := range req.Skills {
		idx++
		req.emit(Event{Phase: "skill", Name: sk.Name, Index: idx, Total: total})
		canonicalDir := filepath.Join(canonicalRoot, sk.Name)

		// Capture the prior manifest so updates can be diffed.
		var oldContent string
		if op == audit.OpUpdate {
			if b, err := os.ReadFile(filepath.Join(canonicalDir, skillManifest)); err == nil {
				oldContent = string(b)
			}
		}

		if err := link.Copy(sk.AbsPath, canonicalDir); err != nil {
			return rep, fmt.Errorf("copy skill %q: %w", sk.Name, err)
		}
		if err := vendorDeps(req.Meta.CrawlRoot, sk.AbsPath, canonicalDir, canonicalRoot); err != nil {
			return rep, fmt.Errorf("vendor files for %q: %w", sk.Name, err)
		}
		if err := sanitize.SkillDir(canonicalDir, req.Options.Security); err != nil {
			return rep, fmt.Errorf("sanitize skill %q: %w", sk.Name, err)
		}
		recordAudit(req.Audit, sk, req.Meta, op, canonicalDir, oldContent)

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
			Version:     req.Meta.Version,
			InstalledAt: req.Now,
			Pinned:      req.Meta.Pinned,
			Kind:        lockfile.KindSkill,
		})
		rep.Skills = append(rep.Skills, sr)
	}

	for _, ref := range req.References {
		idx++
		req.emit(Event{Phase: "reference", Name: ref.Name, Index: idx, Total: total})
		sr, err := installReference(req, lf, ref, op)
		if err != nil {
			return rep, err
		}
		rep.Skills = append(rep.Skills, sr)
	}

	req.emit(Event{Phase: "finalize", Index: total, Total: total})
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
	rep.Audit = req.Audit
	return rep, nil
}

// recordAudit appends an audit change for one installed skill: full content for
// a first install, a unified diff for an update.
func recordAudit(log *audit.Log, sk discover.Skill, meta SourceMeta, op audit.Operation, canonicalDir, oldContent string) {
	newContent := ""
	if b, err := os.ReadFile(filepath.Join(canonicalDir, skillManifest)); err == nil {
		newContent = string(b)
	}
	change := audit.Change{
		Name:      sk.Name,
		Kind:      audit.KindSkill,
		Source:    meta.Source,
		Ref:       meta.Ref,
		SHA:       meta.SHA,
		Operation: op,
	}
	if op == audit.OpUpdate && oldContent != "" {
		d := audit.UnifiedDiff(sk.Name, oldContent, newContent)
		change.Diff = &d
	} else {
		change.Content = &newContent
	}
	log.AddChange(change)
}

// referenceFile is the canonical filename for an installed reference doc.
const referenceFile = "REFERENCE.md"

// installReference sanitizes one reference doc, writes it to the references
// store, records an audit change, and upserts its lockfile entry. References
// get full sanitization (comments stripped too) since they are third-party
// docs rather than authored skills.
func installReference(req Request, lf *lockfile.Lockfile, ref Reference, op audit.Operation) (SkillResult, error) {
	raw, err := os.ReadFile(ref.AbsPath)
	if err != nil {
		return SkillResult{}, fmt.Errorf("read reference %q: %w", ref.Name, err)
	}
	cleaned := sanitize.Reference(string(raw), req.Options.Security)
	dst := filepath.Join(req.BaseDir, ".agents", "references", ref.Name, referenceFile)

	var oldContent string
	if op == audit.OpUpdate {
		if b, err := os.ReadFile(dst); err == nil {
			oldContent = string(b)
		}
	}
	if err := fsutil.AtomicWriteFile(dst, []byte(cleaned), 0o644); err != nil {
		return SkillResult{}, fmt.Errorf("write reference %q: %w", ref.Name, err)
	}

	change := audit.Change{
		Name:      ref.Name,
		Kind:      audit.KindReference,
		Source:    req.Meta.Source,
		Ref:       req.Meta.Ref,
		SHA:       req.Meta.SHA,
		Operation: op,
	}
	if op == audit.OpUpdate && oldContent != "" {
		d := audit.UnifiedDiff(ref.Name, oldContent, cleaned)
		change.Diff = &d
	} else {
		change.Content = &cleaned
	}
	req.Audit.AddChange(change)

	lf.Upsert(lockfile.Entry{
		Name:        ref.Name,
		Source:      req.Meta.Source,
		Subpath:     req.Meta.Subpath,
		File:        ref.File,
		Ref:         req.Meta.Ref,
		SHA:         req.Meta.SHA,
		Version:     req.Meta.Version,
		InstalledAt: req.Now,
		Pinned:      req.Meta.Pinned,
		Kind:        lockfile.KindRef,
	})

	return SkillResult{Name: ref.Name, Kind: lockfile.KindRef}, nil
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
	return refsblock.Rebuild(baseDir, blockGroups(baseDir, lf))
}

// blockGroups assembles the managed-block sections for a lockfile. Reference
// docs always appear; installed skills are added only when an instruction file
// already exists, so a pure-skill install never creates an AGENTS.md (skills are
// auto-discovered via .agents/skills and per-agent links — the block is a
// convenience for files the user already keeps).
func blockGroups(baseDir string, lf *lockfile.Lockfile) []refsblock.Group {
	refs := refsblock.Group{Tag: "references", Links: referenceLinks(baseDir, lf)}
	if _, ok := refsblock.ExistingTarget(baseDir); ok {
		return []refsblock.Group{
			{Tag: "skills", Links: skillLinks(baseDir, lf)},
			refs,
		}
	}
	return []refsblock.Group{refs}
}

// skillLinks builds the managed-block links for a lockfile's skill entries,
// titling each from its installed SKILL.md (falling back to the entry name).
func skillLinks(baseDir string, lf *lockfile.Lockfile) []refsblock.Link {
	var links []refsblock.Link
	for _, e := range lf.Skills {
		if e.Kind != lockfile.KindSkill {
			continue
		}
		p := filepath.Join(baseDir, ".agents", "skills", e.Name, skillManifest)
		title := e.Name
		if data, err := os.ReadFile(p); err == nil {
			title = refsblock.ExtractTitle(data, e.Name)
		}
		links = append(links, refsblock.Link{
			Title: title,
			Path:  "./" + filepath.ToSlash(filepath.Join(".agents", "skills", e.Name, skillManifest)),
		})
	}
	return links
}

// referenceLinks builds the managed-block links for a lockfile's reference
// entries, titling each from its installed REFERENCE.md (falling back to name).
func referenceLinks(baseDir string, lf *lockfile.Lockfile) []refsblock.Link {
	refs := lf.Refs()
	links := make([]refsblock.Link, 0, len(refs))
	for _, e := range refs {
		refPath := filepath.Join(baseDir, ".agents", "references", e.Name, referenceFile)
		title := e.Name
		if data, err := os.ReadFile(refPath); err == nil {
			title = refsblock.ExtractTitle(data, e.Name)
		}
		links = append(links, refsblock.Link{
			Title: title,
			Path:  "./" + filepath.ToSlash(filepath.Join(".agents", "references", e.Name, referenceFile)),
		})
	}
	return links
}

// RebuildReferences regenerates the managed references block from the lockfile.
// It returns the instruction file updated (relative to baseDir), or "" if
// nothing changed. Used by `doctor --fix`.
func RebuildReferences(baseDir string) (string, error) {
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return "", err
	}
	return rebuildReferences(baseDir, lf)
}

// ReferencesInSync reports whether the managed references block matches the
// lockfile's reference entries.
func ReferencesInSync(baseDir string) (bool, error) {
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return false, err
	}
	want := refsblock.ExpectedBlock(blockGroups(baseDir, lf))
	got, exists := refsblock.CurrentBlock(baseDir)
	if want == "" {
		// No references expected: in sync iff there is no block.
		return !exists, nil
	}
	return exists && got == want, nil
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
