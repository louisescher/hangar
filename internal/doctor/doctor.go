// Package doctor diagnoses (and optionally repairs) the on-disk state of a
// Hangar install: lockfile-vs-disk drift, orphaned canonical skill dirs, broken
// or orphaned per-agent symlinks, and a references block out of sync with the
// lockfile.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/fsutil"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/link"
	"github.com/louisescher/hangar/internal/lockfile"
)

// Severity ranks a diagnosis.
type Severity string

const (
	SevInfo  Severity = "info"
	SevWarn  Severity = "warn"
	SevError Severity = "error"
)

// Problem is one diagnosis. When Fixable, fix performs the repair.
type Problem struct {
	Severity Severity
	Message  string
	Fixable  bool
	fix      func() error
}

// Report is the result of a diagnosis.
type Report struct {
	BaseDir  string
	Global   bool
	Detected []agents.Agent
	Problems []Problem
}

// HasIssues reports whether any problem is a warning or error.
func (r Report) HasIssues() bool {
	for _, p := range r.Problems {
		if p.Severity != SevInfo {
			return true
		}
	}
	return false
}

// Fixable counts the auto-repairable problems.
func (r Report) FixableCount() int {
	n := 0
	for _, p := range r.Problems {
		if p.Fixable {
			n++
		}
	}
	return n
}

// MissingEntries returns the names of lockfile entries whose installed files are
// missing on disk (a skill's canonical dir, or a reference's REFERENCE.md). The
// doctor reinstalls these before repairing symlinks, since a restored skill
// recreates its links anyway.
func MissingEntries(baseDir string) ([]string, error) {
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, e := range lf.Skills {
		switch e.Kind {
		case lockfile.KindSkill:
			if !fsutil.IsDir(filepath.Join(baseDir, ".agents", "skills", e.Name)) {
				missing = append(missing, e.Name)
			}
		case lockfile.KindRef:
			if !fsutil.Exists(filepath.Join(baseDir, ".agents", "references", e.Name, "REFERENCE.md")) {
				missing = append(missing, e.Name)
			}
		}
	}
	return missing, nil
}

// Diagnose inspects baseDir for the given scope.
func Diagnose(baseDir string, global bool) (Report, error) {
	rep := Report{BaseDir: baseDir, Global: global}

	detected, err := agents.Detect(global)
	if err != nil {
		return rep, err
	}
	rep.Detected = detected

	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return rep, err
	}

	skillNames := map[string]bool{}
	for _, e := range lf.Skills {
		if e.Kind == lockfile.KindSkill {
			skillNames[e.Name] = true
		}
	}

	canonicalRoot := filepath.Join(baseDir, ".agents", "skills")

	rep.checkLockfileEntries(lf, baseDir, canonicalRoot)
	rep.checkOrphanedSkillDirs(canonicalRoot, skillNames)
	rep.checkAgentLinks(detected, baseDir, canonicalRoot, skillNames, global)
	rep.checkMissingLinks(detected, baseDir, canonicalRoot, lf, global)
	rep.checkReferences(baseDir)
	rep.checkPrefsAgents()

	return rep, nil
}

// checkLockfileEntries flags entries present in the lockfile but missing on disk.
func (r *Report) checkLockfileEntries(lf *lockfile.Lockfile, baseDir, canonicalRoot string) {
	for _, e := range lf.Skills {
		switch e.Kind {
		case lockfile.KindSkill:
			dir := filepath.Join(canonicalRoot, e.Name)
			if !fsutil.IsDir(dir) {
				r.add(Problem{Severity: SevWarn, Message: fmt.Sprintf("skill %q is in the lockfile but missing from .agents/skills (run `hangar update`)", e.Name)})
			}
		case lockfile.KindRef:
			f := filepath.Join(baseDir, ".agents", "references", e.Name, "REFERENCE.md")
			if !fsutil.Exists(f) {
				r.add(Problem{Severity: SevWarn, Message: fmt.Sprintf("reference %q is in the lockfile but its REFERENCE.md is missing (run `hangar update`)", e.Name)})
			}
		}
	}
}

// checkOrphanedSkillDirs flags canonical skill dirs (those with a SKILL.md) that
// are not recorded in the lockfile.
func (r *Report) checkOrphanedSkillDirs(canonicalRoot string, skillNames map[string]bool) {
	entries, err := os.ReadDir(canonicalRoot)
	if err != nil {
		return // no store yet
	}
	for _, e := range entries {
		if !e.IsDir() || skillNames[e.Name()] {
			continue
		}
		// Only flag actual skill dirs; vendored sibling dirs lack a SKILL.md.
		if !fsutil.Exists(filepath.Join(canonicalRoot, e.Name(), "SKILL.md")) {
			continue
		}
		dir := filepath.Join(canonicalRoot, e.Name())
		r.add(Problem{
			Severity: SevWarn,
			Message:  fmt.Sprintf("orphaned skill dir %q (not in the lockfile)", e.Name()),
			Fixable:  true,
			fix:      func() error { return os.RemoveAll(dir) },
		})
	}
}

// checkAgentLinks flags broken or orphaned per-agent symlinks (local scope only;
// global installs are copies).
func (r *Report) checkAgentLinks(detected []agents.Agent, baseDir, canonicalRoot string, skillNames map[string]bool, global bool) {
	if global {
		return
	}
	seen := map[string]bool{}
	for _, ag := range detected {
		dir := filepath.Join(baseDir, ag.InstallPath)
		if filepath.Clean(dir) == filepath.Clean(canonicalRoot) || seen[dir] {
			continue
		}
		seen[dir] = true

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			linkPath := filepath.Join(dir, e.Name())
			fi, err := os.Lstat(linkPath)
			if err != nil || fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(linkPath)
			if err != nil {
				continue
			}
			abs := target
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(dir, target)
			}
			within, _ := fsutil.WithinRoot(canonicalRoot, abs)
			if !within {
				continue // not a hangar-managed link
			}
			lp := linkPath
			switch {
			case !fsutil.Exists(abs):
				r.add(Problem{
					Severity: SevError,
					Message:  fmt.Sprintf("broken symlink %s → %s (target missing)", rel(baseDir, linkPath), target),
					Fixable:  true,
					fix:      func() error { return os.Remove(lp) },
				})
			case !skillNames[e.Name()]:
				r.add(Problem{
					Severity: SevWarn,
					Message:  fmt.Sprintf("orphaned symlink %s (skill not in the lockfile)", rel(baseDir, linkPath)),
					Fixable:  true,
					fix:      func() error { return os.Remove(lp) },
				})
			}
		}
	}
}

// checkMissingLinks flags tracked skills whose canonical dir exists but which
// are not linked into a detected agent (local scope only). Fixable by creating
// the symlink.
func (r *Report) checkMissingLinks(detected []agents.Agent, baseDir, canonicalRoot string, lf *lockfile.Lockfile, global bool) {
	if global {
		return
	}
	for _, ag := range detected {
		dir := filepath.Join(baseDir, ag.InstallPath)
		if filepath.Clean(dir) == filepath.Clean(canonicalRoot) {
			continue // agent installs into the canonical store directly
		}
		for _, e := range lf.Skills {
			if e.Kind != lockfile.KindSkill {
				continue
			}
			canonicalDir := filepath.Join(canonicalRoot, e.Name)
			if !fsutil.IsDir(canonicalDir) {
				continue // missing-from-store is reported elsewhere
			}
			target := filepath.Join(dir, e.Name)
			if fsutil.Exists(target) {
				continue
			}
			cd, tgt := canonicalDir, target
			agName, skName := ag.Def.Name, e.Name
			r.add(Problem{
				Severity: SevWarn,
				Message:  fmt.Sprintf("skill %q is not linked into agent %q", skName, agName),
				Fixable:  true,
				fix:      func() error { return link.Symlink(cd, tgt) },
			})
		}
	}
}

// checkPrefsAgents flags persisted default agents that no longer resolve.
func (r *Report) checkPrefsAgents() {
	prefs, err := config.LoadPrefs()
	if err != nil {
		return
	}
	var invalid []string
	for _, name := range prefs.DefaultAgents {
		if _, _, ok := agents.FindDef(name); !ok {
			invalid = append(invalid, name)
		}
	}
	if len(invalid) == 0 {
		return
	}
	r.add(Problem{
		Severity: SevWarn,
		Message:  "preferences list unknown default agent(s): " + strings.Join(invalid, ", "),
		Fixable:  true,
		fix: func() error {
			p, err := config.LoadPrefs()
			if err != nil {
				return err
			}
			var kept []string
			for _, n := range p.DefaultAgents {
				if _, _, ok := agents.FindDef(n); ok {
					kept = append(kept, n)
				}
			}
			p.DefaultAgents = kept
			return p.Save()
		},
	})
}

// checkReferences flags a references block out of sync with the lockfile.
func (r *Report) checkReferences(baseDir string) {
	inSync, err := install.ReferencesInSync(baseDir)
	if err != nil || inSync {
		return
	}
	r.add(Problem{
		Severity: SevWarn,
		Message:  "the references block is out of sync with the lockfile",
		Fixable:  true,
		fix: func() error {
			_, err := install.RebuildReferences(baseDir)
			return err
		},
	})
}

// Fix applies every fixable problem, returning how many succeeded and any errors.
func (r *Report) Fix() (fixed int, errs []error) {
	for i := range r.Problems {
		p := &r.Problems[i]
		if !p.Fixable || p.fix == nil {
			continue
		}
		if err := p.fix(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Message, err))
			continue
		}
		p.Severity = SevInfo
		p.Message = "fixed: " + p.Message
		p.Fixable = false
		fixed++
	}
	return fixed, errs
}

func (r *Report) add(p Problem) { r.Problems = append(r.Problems, p) }

func rel(base, p string) string {
	if rp, err := filepath.Rel(base, p); err == nil {
		return rp
	}
	return p
}
