package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/security/audit"
	"github.com/louisescher/hangar/internal/security/diff"
	"github.com/louisescher/hangar/internal/security/sanitize"
	"github.com/louisescher/hangar/internal/spec"
)

// InstalledStatus is a lockfile entry paired with its update status.
type InstalledStatus struct {
	Entry    lockfile.Entry
	Latest   string // latest available ref/version (for display)
	Outdated bool
	Gone     bool   // the source no longer exists upstream (deleted/renamed)
	Err      string // other resolution error (offline, etc.), if any
}

// Installed returns the lockfile entries for the given scope.
func (e *Engine) Installed(global bool) ([]lockfile.Entry, error) {
	baseDir, err := resolveBaseDir(InstallOptions{Global: global})
	if err != nil {
		return nil, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return nil, err
	}
	return lf.Skills, nil
}

// resolved caches one source's resolved ref/sha.
type resolved struct {
	ref, sha string
	err      error
}

// CheckUpdates resolves each unpinned entry's latest ref/version and reports
// which are outdated. Pinned entries are reported as up to date (their ref is
// intentional). Sources are resolved once each (de-duped) and concurrently, so a
// monorepo with many skills costs a single resolution.
func (e *Engine) CheckUpdates(ctx context.Context, global bool) ([]InstalledStatus, error) {
	entries, err := e.Installed(global)
	if err != nil {
		return nil, err
	}
	out := make([]InstalledStatus, len(entries))

	// Gather the unique sources that need resolving (unpinned, non-local).
	specs := map[string]spec.SourceSpec{}
	for i, entry := range entries {
		out[i].Entry = entry
		if entry.Pinned {
			out[i].Latest = entryRef(entry)
			continue
		}
		s, err := specFromEntry(entry)
		if err != nil || s.Kind == spec.KindLocal {
			continue
		}
		if _, ok := specs[entry.Source]; !ok {
			specs[entry.Source] = s
		}
	}

	// Resolve each unique source once, concurrently.
	cache := make(map[string]resolved, len(specs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for key, s := range specs {
		wg.Add(1)
		sem <- struct{}{}
		go func(key string, s spec.SourceSpec) {
			defer wg.Done()
			defer func() { <-sem }()
			ref, sha, _, err := e.resolve(ctx, s)
			mu.Lock()
			cache[key] = resolved{ref: ref, sha: sha, err: err}
			mu.Unlock()
		}(key, s)
	}
	wg.Wait()

	// Map results back onto every entry sharing that source.
	for i := range out {
		entry := out[i].Entry
		if entry.Pinned {
			continue
		}
		r, ok := cache[entry.Source]
		if !ok {
			continue // local / unsupported
		}
		if r.err != nil {
			// A deleted/renamed source is reported distinctly so the UI can say
			// "deleted upstream" and keep the skill for manual removal, rather
			// than claiming "up to date" or dropping it.
			if errors.Is(r.err, fetch.ErrNotFound) {
				out[i].Gone = true
			} else {
				out[i].Err = r.err.Error()
			}
			continue
		}
		out[i].Latest = r.ref
		if strings.HasPrefix(entry.Source, "npm:") {
			out[i].Outdated = r.ref != "" && r.ref != entry.Version
		} else {
			out[i].Outdated = r.sha != "" && entry.SHA != "" && r.sha != entry.SHA
		}
	}
	return out, nil
}

// resolve determines a spec's ref/version without downloading its tree.
func (e *Engine) resolve(ctx context.Context, s spec.SourceSpec) (ref, sha string, isTag bool, err error) {
	f, err := e.fetcherFor(s)
	if err != nil {
		return "", "", false, err
	}
	return f.Resolve(ctx, s)
}

// Sync reinstalls every lockfile entry: pinned entries at their exact ref,
// unpinned entries refreshed to the latest. It re-fetches sources and is used by
// `hangar install` with no source (e.g. after cloning a repo with a committed
// lockfile).
func (e *Engine) Sync(ctx context.Context, opt InstallOptions) (install.Report, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return install.Report{}, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return install.Report{}, err
	}
	if len(lf.Skills) == 0 {
		return install.Report{}, fmt.Errorf("the lockfile is empty — run `hangar install <source>` first")
	}
	return e.installEntries(ctx, baseDir, lf.Skills, opt)
}

// Reinstall force-reinstalls the named lockfile entries (re-fetching their
// sources and recreating canonical dirs + per-agent links). Unlike Update it
// does not skip "unchanged" entries, so it restores files that went missing —
// used by `doctor --fix`. Unknown names are ignored.
func (e *Engine) Reinstall(ctx context.Context, names []string, opt InstallOptions) (install.Report, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return install.Report{}, err
	}
	lf, err := lockfile.Load(baseDir)
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
	if len(targets) == 0 {
		return install.Report{}, nil
	}
	return e.installEntries(ctx, baseDir, targets, opt)
}

// ApplyProfile installs a saved set of entries into the current (or global)
// project, re-fetching each source.
func (e *Engine) ApplyProfile(ctx context.Context, entries []lockfile.Entry, opt InstallOptions) (install.Report, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return install.Report{}, err
	}
	if len(entries) == 0 {
		return install.Report{}, fmt.Errorf("profile is empty")
	}
	return e.installEntries(ctx, baseDir, entries, opt)
}

// entryGroup is a set of lockfile entries that share one source fetch. spec is a
// whole-source spec (subpath/file/skill cleared) so a monorepo of many skills is
// downloaded once and every skill is picked out of that single tree.
type entryGroup struct {
	spec    spec.SourceSpec
	entries []lockfile.Entry
}

// sourceFetchKey identifies a unique source download. It deliberately ignores
// subpath/file/skill so per-skill entries of one monorepo share a fetch.
func sourceFetchKey(s spec.SourceSpec) string {
	return strings.Join([]string{
		fmt.Sprint(int(s.Kind)), s.Owner, s.Repo, s.Pkg, s.Path, s.Ref, fmt.Sprint(s.Pinned),
	}, "\x00")
}

// groupEntries batches entries by source so each source is fetched once,
// preserving first-seen order. Entries whose source can't be reconstructed are
// skipped.
func groupEntries(entries []lockfile.Entry) []entryGroup {
	var groups []entryGroup
	index := map[string]int{}
	for _, entry := range entries {
		s, err := specFromEntry(entry)
		if err != nil {
			continue
		}
		// Whole-source fetch: clear the per-entry selectors.
		s.Subpath, s.File, s.Skill = "", "", ""
		key := sourceFetchKey(s)
		if gi, ok := index[key]; ok {
			groups[gi].entries = append(groups[gi].entries, entry)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, entryGroup{spec: s, entries: []lockfile.Entry{entry}})
	}
	return groups
}

// findSkill returns the single discovered skill matching name (or nil).
func findSkill(skills []Skill, name string) []Skill {
	for i := range skills {
		if skills[i].Name == name {
			return []Skill{skills[i]}
		}
	}
	return nil
}

// pickRef returns the single reference doc whose derived name matches (or nil).
func pickRef(refs []discover.RefDoc, pkg, name string) []discover.RefDoc {
	for _, d := range refs {
		if referenceName(pkg, d.RelPath) == name {
			return []discover.RefDoc{d}
		}
	}
	return nil
}

// resolveEntry produces the skills/refs and lockfile meta to install for one
// entry from a group's whole-source discovery d. The entry's own subpath is
// preserved in the meta. If the entry isn't present in d (e.g. a deeply nested
// subpath the root crawl missed) it falls back to a precise per-entry fetch; the
// returned cleanup releases any such fallback download.
func (e *Engine) resolveEntry(ctx context.Context, g entryGroup, d *Discovered, entry lockfile.Entry) (skills []Skill, refs []discover.RefDoc, meta install.SourceMeta, cleanup func(), err error) {
	cleanup = func() {}
	if entry.Kind == lockfile.KindRef {
		refs = pickRef(d.References, g.spec.Pkg, entry.Name)
	} else {
		skills = findSkill(d.Skills, entry.Name)
	}
	if skills != nil || refs != nil {
		// meta.Subpath stays the crawl root (empty for a whole-source fetch); the
		// install layer joins it with each skill's RelPath, reproducing the
		// entry's original subpath exactly — so no churn, no double-nesting.
		return skills, refs, buildMeta(g.spec, d), cleanup, nil
	}

	// Fallback: fetch just this entry at its recorded subpath/file. This also
	// catches a skill that was removed or renamed within a still-existing source
	// — its subpath/name no longer resolves, which we report as "gone".
	es, err := specFromEntry(entry)
	if err != nil {
		return nil, nil, install.SourceMeta{}, cleanup, err
	}
	ed, err := e.Discover(ctx, es)
	if err != nil {
		if errors.Is(err, fetch.ErrNotFound) {
			return nil, nil, install.SourceMeta{}, cleanup, goneError(entry.Name, err)
		}
		return nil, nil, install.SourceMeta{}, cleanup, err
	}
	if entry.Kind == lockfile.KindRef {
		refs = pickRef(ed.References, es.Pkg, entry.Name)
	} else {
		refs = nil
		skills = findSkill(ed.Skills, entry.Name)
	}
	if skills == nil && refs == nil {
		// The source resolved but no longer contains this skill/reference under
		// its name — removed or renamed upstream. Keep it for manual removal.
		_ = ed.Close()
		return nil, nil, install.SourceMeta{}, cleanup, goneError(entry.Name, fetch.ErrNotFound)
	}
	return skills, refs, buildMeta(es, ed), func() { _ = ed.Close() }, nil
}

// goneError wraps fetch.ErrNotFound so callers can detect (via errors.Is) that an
// entry's source/skill vanished upstream and should be skipped + kept, not removed.
func goneError(name string, cause error) error {
	return fmt.Errorf("%q no longer exists at its source: %w", name, cause)
}

// installEntries reinstalls a set of lockfile entries into baseDir. Entries are
// grouped by source so each source is fetched once, then every skill it
// contains is installed from that single download. Progress is emitted per
// source ("fetching") and per entry ("installing").
func (e *Engine) installEntries(ctx context.Context, baseDir string, entries []lockfile.Entry, opt InstallOptions) (install.Report, error) {
	agts, err := resolveAgents(opt.Agents, opt.Global)
	if err != nil {
		return install.Report{}, err
	}

	log := audit.New(audit.OpInstall)
	rep := install.Report{Audit: log}
	installed, failed := map[string]bool{}, map[string]bool{}

	groups := groupEntries(entries)
	total := 0
	for _, g := range groups {
		total += len(g.entries)
	}

	done := 0
	for _, g := range groups {
		if opt.OnProgress != nil {
			opt.OnProgress(install.Event{Phase: "fetching", Name: sourceLabel(g.spec)})
		}
		d, err := e.Discover(ctx, g.spec)
		if err != nil {
			// Source deleted upstream: keep every skill from it and move on.
			if errors.Is(err, fetch.ErrNotFound) {
				for _, entry := range g.entries {
					done++
					rep.Gone = append(rep.Gone, entry.Name)
				}
				continue
			}
			return rep, fmt.Errorf("reinstall from %s: %w", sourceLabel(g.spec), err)
		}

		for _, entry := range g.entries {
			done++
			if opt.OnProgress != nil {
				opt.OnProgress(install.Event{Phase: "installing", Name: entry.Name, Index: done, Total: total})
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
				return rep, fmt.Errorf("reinstall %q: %w", entry.Name, err)
			}

			sub, _ := install.Install(install.Request{
				BaseDir:    baseDir,
				Skills:     skills,
				References: buildReferences(g.spec, refs),
				Agents:     agts,
				Options:    install.Options{Global: opt.Global, Security: opt.Security},
				Operation:  audit.OpInstall,
				Audit:      log,
				Meta:       meta,
			})
			cleanup()
			rep.Skills = append(rep.Skills, sub.Skills...)
			if sub.InstalledInstruction != "" {
				rep.InstalledInstruction = sub.InstalledInstruction
			}
			for _, a := range sub.InstalledAgents {
				installed[a] = true
			}
			for _, a := range sub.FailedAgents {
				failed[a] = true
			}
		}
		d.Close()
	}
	rep.InstalledAgents = setKeys(installed)
	rep.FailedAgents = setKeys(failed)
	return rep, nil
}

// Pin marks an installed entry as pinned. With a non-empty ref it reinstalls the
// skill at that exact ref/version; with an empty ref it just records the intent.
func (e *Engine) Pin(ctx context.Context, name, ref string, opt InstallOptions) (install.Report, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return install.Report{}, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return install.Report{}, err
	}
	entry, ok := lf.Find(name)
	if !ok {
		return install.Report{}, fmt.Errorf("no installed skill named %q", name)
	}

	if ref == "" {
		return install.Report{}, e.SetPinned(name, true, opt)
	}

	s, err := specFromEntry(entry)
	if err != nil {
		return install.Report{}, err
	}
	s.Ref = ref
	s.Pinned = true

	d, err := e.Discover(ctx, s)
	if err != nil {
		return install.Report{}, err
	}
	defer d.Close()
	agts, err := resolveAgents(opt.Agents, opt.Global)
	if err != nil {
		return install.Report{}, err
	}
	var skills []Skill
	var refs []discover.RefDoc
	if entry.Kind == lockfile.KindRef {
		refs = d.References
	} else {
		skills = pickByName(d.Skills, name)
	}
	return install.Install(install.Request{
		BaseDir:    baseDir,
		Skills:     skills,
		References: buildReferences(s, refs),
		Agents:     agts,
		Options:    install.Options{Global: opt.Global, Security: opt.Security},
		Meta:       buildMeta(s, d),
	})
}

// Unpin clears the pinned flag on an installed entry.
func (e *Engine) Unpin(name string, opt InstallOptions) error {
	return e.SetPinned(name, false, opt)
}

// Nuke removes every installed skill and reference, respecting the scope and
// target-agent options. It returns the names removed.
func (e *Engine) Nuke(opt InstallOptions) ([]string, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return nil, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return nil, err
	}
	agts, err := resolveAgents(opt.Agents, opt.Global)
	if err != nil {
		return nil, err
	}
	// Snapshot names first; each Remove rewrites the lockfile.
	names := make([]string, len(lf.Skills))
	for i, e := range lf.Skills {
		names[i] = e.Name
	}
	var removed []string
	for _, name := range names {
		ok, err := install.Remove(baseDir, name, agts, opt.Global)
		if err != nil {
			return removed, err
		}
		if ok {
			removed = append(removed, name)
		}
	}
	return removed, nil
}

// SetPinned records the pinned flag on an installed entry without reinstalling.
func (e *Engine) SetPinned(name string, pinned bool, opt InstallOptions) error {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return err
	}
	if !lf.SetPinned(name, pinned) {
		return fmt.Errorf("no installed skill named %q", name)
	}
	return lf.Save()
}

// PreviewUpdate returns a unified diff of the SKILL.md/REFERENCE.md that an
// update would install against what is currently installed. An empty diff means
// no change.
func (e *Engine) PreviewUpdate(ctx context.Context, name string, opt InstallOptions) (string, error) {
	baseDir, err := resolveBaseDir(opt)
	if err != nil {
		return "", err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return "", err
	}
	entry, ok := lf.Find(name)
	if !ok {
		return "", fmt.Errorf("no installed skill named %q", name)
	}
	s, err := specFromEntry(entry)
	if err != nil {
		return "", err
	}
	d, err := e.Discover(ctx, s)
	if err != nil {
		return "", err
	}
	defer d.Close()

	var newContent, oldPath string
	if entry.Kind == lockfile.KindRef {
		for _, doc := range d.References {
			if referenceName(s.Pkg, doc.RelPath) == name {
				raw, _ := os.ReadFile(doc.AbsPath)
				newContent = sanitize.Reference(string(raw), opt.Security)
				break
			}
		}
		oldPath = filepath.Join(baseDir, ".agents", "references", name, "REFERENCE.md")
	} else {
		for i := range d.Skills {
			if d.Skills[i].Name == name {
				raw, _ := os.ReadFile(filepath.Join(d.Skills[i].AbsPath, "SKILL.md"))
				newContent = sanitize.Skill(string(raw), opt.Security)
				break
			}
		}
		oldPath = filepath.Join(baseDir, ".agents", "skills", name, "SKILL.md")
	}

	oldRaw, _ := os.ReadFile(oldPath)
	return diff.Unified(name, name, string(oldRaw), newContent, 3), nil
}

// InstalledInfo builds an info Discovered for an installed entry by name,
// reading its canonical store. Returns (nil, nil) when no such entry exists.
// The returned Discovered points at persistent files; Close is a no-op.
func (e *Engine) InstalledInfo(name string, global bool) (*Discovered, error) {
	baseDir, err := resolveBaseDir(InstallOptions{Global: global})
	if err != nil {
		return nil, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return nil, err
	}
	entry, ok := lf.Find(name)
	if !ok {
		return nil, nil
	}
	ref := entry.Ref
	if entry.Version != "" {
		ref = entry.Version
	}
	noop := func() error { return nil }

	if entry.Kind == lockfile.KindRef {
		dir := filepath.Join(baseDir, ".agents", "references", name)
		sk := Skill{Name: name, Description: "reference", RelPath: entry.File, AbsPath: dir, IsRef: true}
		return &Discovered{Skills: []Skill{sk}, Source: entry.Source, Ref: ref, Root: dir, cleanup: noop}, nil
	}

	dir := filepath.Join(baseDir, ".agents", "skills", name)
	skills, err := discover.DiscoverSkills(dir)
	if err != nil || len(skills) == 0 {
		skills = []Skill{{Name: name, AbsPath: dir}}
	}
	return &Discovered{Skills: skills, Source: entry.Source, Ref: ref, Root: dir, cleanup: noop}, nil
}

// InstalledDoc returns the manifest (SKILL.md / REFERENCE.md) of an installed
// entry by name, and whether it was found.
func (e *Engine) InstalledDoc(name string, global bool) (string, bool, error) {
	baseDir, err := resolveBaseDir(InstallOptions{Global: global})
	if err != nil {
		return "", false, err
	}
	lf, err := lockfile.Load(baseDir)
	if err != nil {
		return "", false, err
	}
	entry, ok := lf.Find(name)
	if !ok {
		return "", false, nil
	}
	var p string
	if entry.Kind == lockfile.KindRef {
		p = filepath.Join(baseDir, ".agents", "references", name, "REFERENCE.md")
	} else {
		p = filepath.Join(baseDir, ".agents", "skills", name, "SKILL.md")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func entryRef(e lockfile.Entry) string {
	if e.Version != "" {
		return e.Version
	}
	return e.Ref
}

func setKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
