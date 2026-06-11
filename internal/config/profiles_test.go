package config

import (
	"testing"

	"github.com/louisescher/hangar/internal/lockfile"
)

func TestProfileRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	p := Profile{Name: "work", Skills: []lockfile.Entry{
		{Name: "pdf", Source: "anthropics/skills", Subpath: "skills/pdf", Ref: "main", Kind: lockfile.KindSkill},
		{Name: "is-odd", Source: "npm:is-odd", Version: "3.0.1", Kind: lockfile.KindRef},
	}}
	if err := SaveProfile(p); err != nil {
		t.Fatal(err)
	}

	got, err := LoadProfile("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 || got.Skills[0].Name != "pdf" || got.Skills[1].Source != "npm:is-odd" {
		t.Errorf("round-trip mismatch: %+v", got.Skills)
	}

	names, err := ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "work" {
		t.Errorf("ListProfiles = %v, want [work]", names)
	}

	removed, err := RemoveProfile("work")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	if _, err := LoadProfile("work"); err == nil {
		t.Error("expected error loading a removed profile")
	}
}

func TestProfileInvalidName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveProfile(Profile{Name: "../escape"}); err == nil {
		t.Error("expected an error for a path-traversing profile name")
	}
	if _, err := LoadProfile("bad/name"); err == nil {
		t.Error("expected an error for an invalid profile name")
	}
}
