package discover

import (
	"os"
	"path/filepath"
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

func TestSplitFrontmatter(t *testing.T) {
	fm, body := SplitFrontmatter([]byte("---\nname: pdf\ndescription: Fill PDFs\n---\n# Heading\n\nBody"))
	if string(fm) != "name: pdf\ndescription: Fill PDFs\n" {
		t.Errorf("frontmatter = %q", string(fm))
	}
	if string(body) != "# Heading\n\nBody" {
		t.Errorf("body = %q", string(body))
	}

	// No frontmatter.
	fm, body = SplitFrontmatter([]byte("# Just markdown"))
	if len(fm) != 0 || string(body) != "# Just markdown" {
		t.Errorf("expected empty fm and full body, got fm=%q body=%q", fm, body)
	}

	// BOM tolerance.
	fm, _ = SplitFrontmatter([]byte("\xEF\xBB\xBF---\nname: x\n---\nbody"))
	if string(fm) != "name: x\n" {
		t.Errorf("BOM frontmatter = %q", string(fm))
	}
}

func TestDiscoverRootSkill(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "SKILL.md"), "---\nname: solo\ndescription: A solo skill\n---\nbody")

	got, err := DiscoverSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "solo" || got[0].Description != "A solo skill" {
		t.Fatalf("got %+v", got)
	}
	if got[0].RelPath != "" {
		t.Errorf("root skill RelPath = %q, want empty", got[0].RelPath)
	}
}

func TestDiscoverSkillsDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "skills", "pdf", "SKILL.md"), "---\nname: pdf\ndescription: PDFs\n---\n")
	write(t, filepath.Join(root, "skills", "web", "scraping", "SKILL.md"), "---\nname: scraping\n---\n")
	// A README at root should NOT be picked up as a skill.
	write(t, filepath.Join(root, "README.md"), "# repo")

	got, err := DiscoverSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(got), got)
	}
	byName := map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if byName["pdf"].RelPath != "skills/pdf" {
		t.Errorf("pdf RelPath = %q", byName["pdf"].RelPath)
	}
	if byName["scraping"].RelPath != "skills/web/scraping" {
		t.Errorf("scraping RelPath = %q", byName["scraping"].RelPath)
	}
}

func TestDiscoverNameFallbackToDir(t *testing.T) {
	root := t.TempDir()
	// No name in frontmatter -> fall back to directory name.
	write(t, filepath.Join(root, "skills", "my-tool", "SKILL.md"), "---\ndescription: no name field\n---\n")
	got, err := DiscoverSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "my-tool" {
		t.Fatalf("got %+v, want name=my-tool", got)
	}
}

func TestDiscoverWholeTreeFallback(t *testing.T) {
	root := t.TempDir()
	// No root SKILL.md and no skills/ dir: should fall back to a full scan.
	write(t, filepath.Join(root, "packages", "a", "SKILL.md"), "---\nname: a\n---\n")
	write(t, filepath.Join(root, ".hidden", "SKILL.md"), "---\nname: hidden\n---\n")

	got, err := DiscoverSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("got %+v, want only skill 'a' (dot-dirs skipped)", got)
	}
}

func TestCollectRefDocs(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "README.md"), "# readme")
	write(t, filepath.Join(root, "docs", "guide.md"), "# guide")
	write(t, filepath.Join(root, "docs", "nested", "deep.md"), "# deep")
	write(t, filepath.Join(root, "src", "ignored.md"), "# ignored")

	got, err := CollectRefDocs(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	rels := map[string]bool{}
	for _, d := range got {
		rels[d.RelPath] = true
	}
	for _, want := range []string{"README.md", "docs/guide.md", "docs/nested/deep.md"} {
		if !rels[want] {
			t.Errorf("expected %q in default ref docs, got %v", want, rels)
		}
	}
	if rels["src/ignored.md"] {
		t.Error("src/ignored.md should not be in default scope")
	}

	// Include list overrides default scope.
	got, err = CollectRefDocs(root, []string{"src/ignored.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RelPath != "src/ignored.md" {
		t.Fatalf("include override got %+v", got)
	}
}
