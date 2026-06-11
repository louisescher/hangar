package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnoseFindsAndFixes(t *testing.T) {
	base := t.TempDir()
	// Point HOME at base so agent auto-detection sees the .claude dir we create
	// below (and nothing from the real machine).
	t.Setenv("HOME", base)
	canonical := filepath.Join(base, ".agents", "skills")

	// A legitimate, lockfile-tracked skill.
	write(t, filepath.Join(canonical, "pdf", "SKILL.md"), "---\nname: pdf\n---\n")
	// An orphaned skill dir (has SKILL.md, not in the lockfile).
	write(t, filepath.Join(canonical, "ghost", "SKILL.md"), "---\nname: ghost\n---\n")
	// A vendored sibling dir (no SKILL.md) must NOT be flagged as orphaned.
	write(t, filepath.Join(canonical, "shared", "ref.md"), "# shared")

	// Lockfile tracks only pdf.
	write(t, filepath.Join(base, ".agents", "hangar.lock"),
		"version = 1\n[[skills]]\nname=\"pdf\"\nsource=\"o/r\"\ninstalled_at=2026-06-11T00:00:00Z\npinned=false\nkind=\"skill\"\n")

	// A broken symlink in a (forced-detected) agent dir.
	agentDir := filepath.Join(base, ".claude", "skills")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(agentDir, "pdf")
	if err := os.Symlink(filepath.Join(canonical, "does-not-exist"), broken); err != nil {
		t.Fatal(err)
	}

	rep, err := Diagnose(base, false)
	if err != nil {
		t.Fatal(err)
	}

	// Find the orphaned dir warning and the broken symlink error.
	var sawOrphan, sawBroken, sawShared bool
	for _, p := range rep.Problems {
		if strings.Contains(p.Message, "ghost") {
			sawOrphan = true
		}
		if strings.Contains(p.Message, "broken symlink") {
			sawBroken = true
		}
		if strings.Contains(p.Message, "shared") {
			sawShared = true
		}
	}
	if !sawOrphan {
		t.Errorf("expected an orphaned-dir problem for ghost; problems: %+v", rep.Problems)
	}
	if sawShared {
		t.Error("vendored sibling dir 'shared' should not be flagged as orphaned")
	}
	if !rep.HasIssues() {
		t.Error("expected HasIssues to be true")
	}
	if !sawBroken {
		t.Errorf("expected a broken-symlink problem; problems: %+v", rep.Problems)
	}

	// Fix repairs the orphaned dir.
	fixed, errs := rep.Fix()
	if len(errs) != 0 {
		t.Fatalf("fix errors: %v", errs)
	}
	if fixed == 0 {
		t.Fatal("expected at least one fix")
	}
	if _, err := os.Stat(filepath.Join(canonical, "ghost")); !os.IsNotExist(err) {
		t.Errorf("ghost dir should have been removed, stat err = %v", err)
	}
	// The tracked skill survives.
	if _, err := os.Stat(filepath.Join(canonical, "pdf", "SKILL.md")); err != nil {
		t.Errorf("tracked pdf skill must survive: %v", err)
	}
}

func TestHealthyIsQuiet(t *testing.T) {
	base := t.TempDir()
	rep, err := Diagnose(base, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HasIssues() {
		t.Errorf("empty project should be healthy, got: %+v", rep.Problems)
	}
}
