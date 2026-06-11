package lockfile

import (
	"testing"
	"time"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	lf, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if lf.Version != SchemaVersion || len(lf.Skills) != 0 {
		t.Fatalf("expected empty lockfile, got %+v", lf)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	lf, _ := Load(dir)
	ts := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	lf.Upsert(Entry{Name: "pdf", Source: "anthropics/skills", Subpath: "skills/pdf", Ref: "v1.2.0", SHA: "abc", InstalledAt: ts, Pinned: true, Kind: KindSkill})
	lf.Upsert(Entry{Name: "guide", Source: "npm:zod", File: "README.md", InstalledAt: ts, Kind: KindRef})
	if err := lf.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Skills))
	}
	// Entries are sorted by name on save.
	if got.Skills[0].Name != "guide" || got.Skills[1].Name != "pdf" {
		t.Errorf("unexpected order: %v", []string{got.Skills[0].Name, got.Skills[1].Name})
	}
	pdf, ok := got.Find("pdf")
	if !ok || pdf.Ref != "v1.2.0" || !pdf.Pinned || !pdf.InstalledAt.Equal(ts) {
		t.Errorf("pdf round-trip mismatch: %+v", pdf)
	}
	if refs := got.Refs(); len(refs) != 1 || refs[0].Name != "guide" {
		t.Errorf("Refs() = %+v", refs)
	}
}

func TestUpsertReplacesAndRemove(t *testing.T) {
	lf := &Lockfile{Version: SchemaVersion}
	lf.Upsert(Entry{Name: "pdf", Ref: "v1"})
	lf.Upsert(Entry{Name: "pdf", Ref: "v2"})
	if len(lf.Skills) != 1 || lf.Skills[0].Ref != "v2" {
		t.Fatalf("upsert should replace by name: %+v", lf.Skills)
	}
	if !lf.Remove("pdf") || lf.Remove("pdf") {
		t.Error("Remove should report true once, then false")
	}
}
