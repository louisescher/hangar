package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONShape(t *testing.T) {
	l := New(OpInstall)
	content := "# skill"
	l.AddChange(Change{Name: "pdf", Kind: KindSkill, Source: "o/r", Ref: "v1", SHA: "abc", Operation: OpInstall, Content: &content})
	l.AddFinding(TagRewriteFinding("pdf", "v1", "old", "new"))

	raw, err := l.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["schemaVersion"].(float64) != 1 || obj["command"].(string) != "install" {
		t.Errorf("unexpected header: %v", obj)
	}
	changes := obj["changes"].([]any)
	c0 := changes[0].(map[string]any)
	// content present, diff explicitly null.
	if c0["content"].(string) != "# skill" {
		t.Errorf("content = %v", c0["content"])
	}
	if c0["diff"] != nil {
		t.Errorf("diff should be null on install, got %v", c0["diff"])
	}
	findings := obj["findings"].([]any)
	f0 := findings[0].(map[string]any)
	if f0["severity"].(string) != "high" || f0["kind"].(string) != "tag_rewritten" {
		t.Errorf("finding = %v", f0)
	}
}

func TestStdoutEnvelope(t *testing.T) {
	l := New(OpUpdate)
	d := UnifiedDiff("pdf", "old\n", "new\n")
	l.AddChange(Change{Name: "pdf", Kind: KindSkill, Operation: OpUpdate, Diff: &d})

	env := l.StdoutEnvelope()
	if !strings.Contains(env, "=== hangar audit ===") || !strings.Contains(env, "=== end hangar audit ===") {
		t.Errorf("envelope markers missing:\n%s", env)
	}
	if !strings.Contains(env, "third-party content under") {
		t.Errorf("envelope missing untrusted-content warning:\n%s", env)
	}
	if !strings.Contains(env, "\"command\":\"update\"") {
		t.Errorf("envelope JSON missing command:\n%s", env)
	}
}

func TestEmpty(t *testing.T) {
	if !New(OpInstall).Empty() {
		t.Error("a fresh log should be empty")
	}
}
