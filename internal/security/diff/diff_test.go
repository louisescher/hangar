package diff

import (
	"strings"
	"testing"
)

func TestUnifiedEqualIsEmpty(t *testing.T) {
	if got := Unified("a/x", "b/x", "same\n", "same\n", 3); got != "" {
		t.Errorf("equal inputs should diff to empty, got %q", got)
	}
}

func TestUnifiedSimpleChange(t *testing.T) {
	old := "line1\nline2\nline3\n"
	neu := "line1\nCHANGED\nline3\n"
	got := Unified("a/x", "b/x", old, neu, 3)

	for _, want := range []string{"--- a/x", "+++ b/x", "@@", "-line2", "+CHANGED", " line1", " line3"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q:\n%s", want, got)
		}
	}
}

func TestUnifiedNoTrailingNewline(t *testing.T) {
	got := Unified("a/x", "b/x", "a\nb", "a\nc", 3)
	if !strings.Contains(got, noNewline) {
		t.Errorf("expected no-newline marker:\n%s", got)
	}
}

func TestUnifiedPureInsertAndDelete(t *testing.T) {
	got := Unified("a/x", "b/x", "a\n", "a\nb\n", 3)
	if !strings.Contains(got, "+b") {
		t.Errorf("expected insertion of b:\n%s", got)
	}
	got = Unified("a/x", "b/x", "a\nb\n", "a\n", 3)
	if !strings.Contains(got, "-b") {
		t.Errorf("expected deletion of b:\n%s", got)
	}
}
