package sanitize

import "testing"

// Invisible characters under test, constructed numerically so the source file
// contains no literal invisible bytes.
var (
	zwsp = string(rune(0x200b))  // zero-width space
	rlo  = string(rune(0x202e))  // right-to-left override (bidi)
	bom  = string(rune(0xfeff))  // byte-order mark
	tag  = string(rune(0xe0041)) // Unicode Tag block
)

func TestStripInvisible(t *testing.T) {
	// Zero-width space and bidi override are removed.
	if got := stripInvisible("he" + zwsp + "llo" + rlo + "world"); got != "helloworld" {
		t.Errorf("got %q, want helloworld", got)
	}

	// A leading BOM is allowed; a non-leading BOM is stripped.
	if got := stripInvisible(bom + "hi"); got != bom+"hi" {
		t.Errorf("leading BOM should be kept, got %q", got)
	}
	if got := stripInvisible("hi" + bom + "there"); got != "hithere" {
		t.Errorf("non-leading BOM should be stripped, got %q", got)
	}

	// Unicode Tag block is removed.
	if got := stripInvisible("a" + tag + "b"); got != "ab" {
		t.Errorf("tag block should be stripped, got %q", got)
	}
}

func TestSkillKeepsCommentsStripsInvisible(t *testing.T) {
	in := "<!-- keep me -->" + zwsp + " text"
	if got := Skill(in, All); got != "<!-- keep me --> text" {
		t.Errorf("Skill should keep comments but strip invisible: %q", got)
	}
}

func TestStripCommentsHTML(t *testing.T) {
	in := "before <!-- secret --> after\n"
	if got := stripComments(in); got != "before  after\n" {
		t.Errorf("got %q", got)
	}

	// Multi-line HTML comment. The newline of the opening line is preserved
	// (matching rosie), so a blank line remains where the comment began.
	in = "a\n<!-- line1\nline2 -->b\nc\n"
	want := "a\n\nb\nc\n"
	if got := stripComments(in); got != want {
		t.Errorf("multiline: got %q, want %q", got, want)
	}
}

func TestStripCommentsLinkForm(t *testing.T) {
	in := "keep\n[//]: # \"hidden comment\"\nkeep2\n"
	want := "keep\nkeep2\n"
	if got := stripComments(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripCommentsPreservesFencedCode(t *testing.T) {
	in := "```\n<!-- this is sample code, keep it -->\n```\n"
	if got := stripComments(in); got != in {
		t.Errorf("fenced content must be preserved, got %q", got)
	}
}

func TestReferenceStripsBoth(t *testing.T) {
	in := "<!-- c -->vis" + zwsp + "ible\n"
	if got := Reference(in, All); got != "visible\n" {
		t.Errorf("got %q, want %q", got, "visible\n")
	}
}
