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

	// A README H1 full of badges must clean down to plain text so it can sit
	// inside an outer [title](path) link without breaking the markdown.
	badge := "# is-odd [![NPM version](https://img.shields.io/npm/v/is-odd.svg)](https://npmjs.com/package/is-odd) [![Build](https://x/b.svg)](https://x/b)\n"
	if got := ExtractTitle([]byte(badge), "is-odd"); got != "is-odd" {
		t.Errorf("badge title = %q, want is-odd", got)
	}
	// An H1 that is nothing but a badge cleans to empty → fall back to the name.
	if got := ExtractTitle([]byte("# [![only a badge](u)](u)\n"), "pkgname"); got != "pkgname" {
		t.Errorf("empty cleaned title should fall back, got %q", got)
	}
	// A plain link unwraps to its text.
	if got := ExtractTitle([]byte("# [Zod](https://zod.dev)\n"), "fb"); got != "Zod" {
		t.Errorf("link title = %q, want Zod", got)
	}
}

func TestReplaceBlockAddReplaceRemove(t *testing.T) {
	refs := func(ls ...Link) []Group { return []Group{{Tag: "references", Links: ls}} }

	// Add to existing prose.
	out, changed := replaceBlock("# Handbook\n\nText.\n", buildBody(refs(Link{Title: "A", Path: "./a"})))
	if !changed || !strings.Contains(out, startMarker) || !strings.Contains(out, "[A](./a)") {
		t.Fatalf("add failed: %q", out)
	}

	// Replace existing block contents.
	out2, changed := replaceBlock(out, buildBody(refs(Link{Title: "B", Path: "./b"})))
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
	target, err := Rebuild(dir, []Group{{Tag: "references", Links: []Link{{Title: "Zod", Path: "./.agents/references/zod/REFERENCE.md"}}}})
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
	target, err := Rebuild(dir, []Group{{Tag: "references", Links: []Link{{Title: "X", Path: "./x"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if target != "CLAUDE.md" {
		t.Errorf("target = %q, want CLAUDE.md (existing file preferred)", target)
	}
}
