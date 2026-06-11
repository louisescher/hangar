package github

import "testing"

func TestParseTag(t *testing.T) {
	tests := []struct {
		tag, repo       string
		wantOK          bool
		wantPrefixed    bool
		maj, min, patch int
		pre             string
	}{
		{"v1.2.3", "repo", true, false, 1, 2, 3, ""},
		{"1.2.3", "repo", true, false, 1, 2, 3, ""},
		{"v0.5", "repo", true, false, 0, 5, 0, ""},
		{"v1.2.3-rc1", "repo", true, false, 1, 2, 3, "rc1"},
		{"skills@v2.0.0", "skills", true, true, 2, 0, 0, ""},
		{"skills-v2.1.0", "skills", true, true, 2, 1, 0, ""},
		{"not-a-version", "repo", false, false, 0, 0, 0, ""},
		{"release", "repo", false, false, 0, 0, 0, ""},
	}
	for _, tt := range tests {
		v, prefixed, ok := parseTag(tt.tag, tt.repo)
		if ok != tt.wantOK || prefixed != tt.wantPrefixed {
			t.Errorf("parseTag(%q,%q) ok=%v prefixed=%v, want ok=%v prefixed=%v", tt.tag, tt.repo, ok, prefixed, tt.wantOK, tt.wantPrefixed)
			continue
		}
		if ok && (v.major != tt.maj || v.minor != tt.min || v.patch != tt.patch || v.pre != tt.pre) {
			t.Errorf("parseTag(%q) = %+v, want %d.%d.%d-%q", tt.tag, v, tt.maj, tt.min, tt.patch, tt.pre)
		}
	}
}

func TestSelectLatestTag(t *testing.T) {
	// Bare tags only: highest non-prerelease wins.
	tags := map[string]string{
		"v1.0.0":     "sha100",
		"v1.2.0":     "sha120",
		"v1.3.0-rc1": "sha130rc", // prerelease, ignored
	}
	tag, sha, ok := selectLatestTag(tags, "repo")
	if !ok || tag != "v1.2.0" || sha != "sha120" {
		t.Errorf("got tag=%q sha=%q ok=%v, want v1.2.0/sha120", tag, sha, ok)
	}

	// Monorepo precedence: prefixed tags win even if a bare tag is numerically higher.
	tags = map[string]string{
		"v9.9.9":        "shabare",
		"skills@v2.0.0": "shapfx20",
		"skills@v2.1.0": "shapfx21",
	}
	tag, sha, ok = selectLatestTag(tags, "skills")
	if !ok || tag != "skills@v2.1.0" || sha != "shapfx21" {
		t.Errorf("got tag=%q sha=%q ok=%v, want skills@v2.1.0/shapfx21", tag, sha, ok)
	}

	// No semver tags at all.
	if _, _, ok := selectLatestTag(map[string]string{"latest": "x"}, "repo"); ok {
		t.Error("expected no selection for non-semver tags")
	}
}
