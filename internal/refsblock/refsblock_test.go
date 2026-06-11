package refsblock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTitle(t *testing.T) {
	if got := ExtractTitle([]byte("---\nname: x\n---\n# Real Title\n\nbody"), "fb"); got != "Real Title" {
		t.Errorf("got %q", got)
	}
	if got := ExtractTitle([]byte("no heading here"), "fallback"); got != "fallback" {
		t.Errorf("got %q, want fallback", got)
	}
}

func TestReplaceBlockAddReplaceRemove(t *testing.T) {
	// Add to existing prose.
	out, changed := replaceBlock("# Handbook\n\nText.\n", buildBody([]Link{{Title: "A", Path: "./a"}}))
	if !changed || !strings.Contains(out, startMarker) || !strings.Contains(out, "[A](./a)") {
		t.Fatalf("add failed: %q", out)
	}

	// Replace existing block contents.
	out2, changed := replaceBlock(out, buildBody([]Link{{Title: "B", Path: "./b"}}))
	if !changed || strings.Contains(out2, "[A](./a)") || !strings.Contains(out2, "[B](./b)") {
		t.Fatalf("replace failed: %q", out2)
	}
	if strings.Count(out2, startMarker) != 1 {
		t.Errorf("expected exactly one block, got %d", strings.Count(out2, startMarker))
	}

	// Remove the block when there are no links.
	out3, changed := replaceBlock(out2, buildBody(nil))
	if !changed || strings.Contains(out3, startMarker) {
		t.Fatalf("remove failed: %q", out3)
	}
	if !strings.Contains(out3, "# Handbook") {
		t.Errorf("user prose should be preserved: %q", out3)
	}
}

func TestRebuildCreatesAgentsFile(t *testing.T) {
	dir := t.TempDir()
	target, err := Rebuild(dir, []Link{{Title: "Zod", Path: "./.agents/references/zod/REFERENCE.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if target != "AGENTS.md" {
		t.Errorf("target = %q, want AGENTS.md", target)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[Zod](./.agents/references/zod/REFERENCE.md)") {
		t.Errorf("AGENTS.md missing link: %q", data)
	}
}

func TestRebuildPrefersExistingClaudeMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := Rebuild(dir, []Link{{Title: "X", Path: "./x"}})
	if err != nil {
		t.Fatal(err)
	}
	if target != "CLAUDE.md" {
		t.Errorf("target = %q, want CLAUDE.md (existing file preferred)", target)
	}
}
