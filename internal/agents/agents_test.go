package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Defs {
		if d.Name == "" {
			t.Errorf("def with empty name: %+v", d)
		}
		if seen[d.Name] {
			t.Errorf("duplicate agent name %q", d.Name)
		}
		seen[d.Name] = true
		if d.ProjectPath == "" || d.GlobalPath == "" {
			t.Errorf("agent %q missing install path(s)", d.Name)
		}
		if d.Display == "" {
			t.Errorf("agent %q missing display name", d.Name)
		}
	}
	if !seen["claude"] || !seen["universal"] {
		t.Error("expected claude and universal agents to be present")
	}
}

func TestUniversalIsTargetOnly(t *testing.T) {
	def, _, ok := FindDef("universal")
	if !ok {
		t.Fatal("universal not found")
	}
	if def.DetectDir != "" {
		t.Errorf("universal should have empty DetectDir, got %q", def.DetectDir)
	}
}

func TestFindDefAlias(t *testing.T) {
	def, alias, ok := FindDef("amplify")
	if !ok {
		t.Fatal("alias amplify not resolved")
	}
	if def.Name != "augment" {
		t.Errorf("amplify should resolve to augment, got %q", def.Name)
	}
	if alias != "amplify" {
		t.Errorf("expected alias to be reported, got %q", alias)
	}

	if _, _, ok := FindDef("does-not-exist"); ok {
		t.Error("unexpected match for unknown agent")
	}
}

func TestDetect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{".claude", ".cursor", ".config/opencode"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Detect(false)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, a := range got {
		names[a.Def.Name] = a.InstallPath
	}
	for _, want := range []string{"claude", "cursor", "opencode"} {
		if _, ok := names[want]; !ok {
			t.Errorf("expected %q to be detected", want)
		}
	}
	if names["claude"] != ".claude/skills" {
		t.Errorf("local install path = %q, want .claude/skills", names["claude"])
	}
	if _, ok := names["universal"]; ok {
		t.Error("universal should never be auto-detected")
	}
}

func TestResolveGlobalAndAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	out, aliases, err := Resolve([]string{"claude", "amplify"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(out))
	}
	if aliases["amplify"] != "augment" {
		t.Errorf("expected amplify->augment alias record, got %v", aliases)
	}
	wantClaude := filepath.Join(home, ".claude/skills")
	if out[0].InstallPath != wantClaude {
		t.Errorf("global claude path = %q, want %q", out[0].InstallPath, wantClaude)
	}

	if _, _, err := Resolve([]string{"nope"}, false); err == nil {
		t.Error("expected error for unknown agent")
	}
}
