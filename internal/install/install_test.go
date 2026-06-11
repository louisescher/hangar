package install

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/lockfile"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallEndToEnd(t *testing.T) {
	// Fixture source: a skill that references a sibling file outside its dir.
	src := t.TempDir()
	skillDir := filepath.Join(src, "skills", "pdf")
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: pdf\ndescription: PDFs\n---\nSee [shared](../shared/ref.md).\n")
	writeFile(t, filepath.Join(skillDir, "reference.md"), "# bundled")
	writeFile(t, filepath.Join(src, "skills", "shared", "ref.md"), "# shared ref")

	base := t.TempDir()

	// One healthy agent, one blocked agent (a real dir occupies the link path).
	blockedTarget := filepath.Join(base, ".blocked", "skills", "pdf")
	writeFile(t, filepath.Join(blockedTarget, "placeholder"), "x")

	agts := []agents.Agent{
		{Def: agents.Def{Name: "claude"}, InstallPath: ".claude/skills", Detected: true},
		{Def: agents.Def{Name: "blocked"}, InstallPath: ".blocked/skills", Detected: true},
	}

	rep, err := Install(Request{
		BaseDir: base,
		Skills:  []discover.Skill{{Name: "pdf", Description: "PDFs", RelPath: "pdf", AbsPath: skillDir}},
		Agents:  agts,
		Meta:    SourceMeta{Source: "owner/repo", Subpath: "skills", Ref: "v1.0.0", SHA: "deadbeef", IsTag: true, Pinned: true, CrawlRoot: src},
		Now:     time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Install returned a top-level error: %v", err)
	}

	// Canonical copy + bundled file.
	if _, err := os.Stat(filepath.Join(base, ".agents", "skills", "pdf", "SKILL.md")); err != nil {
		t.Errorf("canonical SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, ".agents", "skills", "pdf", "reference.md")); err != nil {
		t.Errorf("bundled reference.md missing: %v", err)
	}

	// Vendored sibling file, placed so the relative link still resolves.
	if _, err := os.Stat(filepath.Join(base, ".agents", "skills", "shared", "ref.md")); err != nil {
		t.Errorf("vendored shared/ref.md missing: %v", err)
	}

	// Healthy agent got a symlink to the canonical store.
	linkPath := filepath.Join(base, ".claude", "skills", "pdf")
	fi, err := os.Lstat(linkPath)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected a symlink at %s (err=%v)", linkPath, err)
	}

	// Blocked agent recorded as failed; install still succeeded overall.
	if len(rep.Skills) != 1 {
		t.Fatalf("expected 1 skill result, got %d", len(rep.Skills))
	}
	if _, blocked := rep.Skills[0].FailedAgents["blocked"]; !blocked {
		t.Errorf("expected 'blocked' in FailedAgents, got %v", rep.Skills[0].FailedAgents)
	}
	if !contains(rep.Skills[0].InstalledAgents, "claude") {
		t.Errorf("expected 'claude' in InstalledAgents, got %v", rep.Skills[0].InstalledAgents)
	}

	// Lockfile entry.
	lf, err := lockfile.Load(base)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := lf.Find("pdf")
	if !ok {
		t.Fatal("lockfile entry for pdf missing")
	}
	if e.Source != "owner/repo" || e.Subpath != "skills/pdf" || e.Kind != lockfile.KindSkill || !e.Pinned {
		t.Errorf("unexpected lockfile entry: %+v", e)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
