package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/louisescher/hangar/internal/agents"
	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/security/sanitize"
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

func TestInstallReferences(t *testing.T) {
	// Two reference docs sourced from an npm package.
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "README.md"), "# Demo\nVisible.<!-- hidden -->\n")
	writeFile(t, filepath.Join(src, "docs", "api.md"), "# API\nDetails.\n")

	base := t.TempDir()
	rep, err := Install(Request{
		BaseDir: base,
		References: []Reference{
			{Name: "demo", AbsPath: filepath.Join(src, "README.md"), File: "README.md"},
			{Name: "demo-api", AbsPath: filepath.Join(src, "docs", "api.md"), File: "docs/api.md"},
		},
		Options: Options{Security: sanitize.All},
		Meta:    SourceMeta{Source: "npm:demo", Version: "1.2.0", Pinned: true},
		Now:     time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// REFERENCE.md written for each, with reference sanitization (comment gone).
	readmePath := filepath.Join(base, ".agents", "references", "demo", "REFERENCE.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reference REFERENCE.md missing: %v", err)
	}
	if string(data) != "# Demo\nVisible.\n" {
		t.Errorf("reference not sanitized: %q", data)
	}
	if _, err := os.Stat(filepath.Join(base, ".agents", "references", "demo-api", "REFERENCE.md")); err != nil {
		t.Errorf("second reference missing: %v", err)
	}

	// Lockfile ref entries.
	lf, err := lockfile.Load(base)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := lf.Find("demo")
	if !ok {
		t.Fatal("lockfile entry for demo missing")
	}
	if e.Kind != lockfile.KindRef || e.Source != "npm:demo" || e.Version != "1.2.0" || e.File != "README.md" {
		t.Errorf("unexpected ref entry: %+v", e)
	}
	if len(lf.Refs()) != 2 {
		t.Errorf("expected 2 ref entries, got %d", len(lf.Refs()))
	}

	// Report records references with the ref kind.
	if len(rep.Skills) != 2 {
		t.Fatalf("expected 2 results, got %d", len(rep.Skills))
	}
	for _, sr := range rep.Skills {
		if sr.Kind != lockfile.KindRef {
			t.Errorf("result %q kind = %q, want ref", sr.Name, sr.Kind)
		}
	}

	// Instructions block now links to the references.
	if rep.InstalledInstruction == "" {
		t.Error("expected an instruction file to be updated with the references block")
	}
	agentsMD, err := os.ReadFile(filepath.Join(base, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md missing: %v", err)
	}
	if !strings.Contains(string(agentsMD), ".agents/references/demo/REFERENCE.md") {
		t.Errorf("references block missing demo link:\n%s", agentsMD)
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
