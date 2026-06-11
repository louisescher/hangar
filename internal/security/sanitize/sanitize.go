// Package sanitize removes content that an LLM reads but a human reviewer can't
// easily see, or that hides instructions from review:
//
//   - StripInvisible: zero-width characters, the Unicode Tag block, and
//     bidi override/isolate codepoints (the "Trojan Source" class).
//   - StripComments: markdown comments outside fenced code blocks — HTML form
//     (<!-- ... -->, possibly multi-line) and link form ([//]: # "...").
//
// Skills are sanitized for invisible characters only; references additionally
// have comments stripped. Ported from withastro/rosie (sanitize.rs/.ts).
package sanitize

import (
	"os"
	"path/filepath"
	"strings"
)

// Opts selects which sanitization passes to run.
type Opts struct {
	StripComments  bool
	StripInvisible bool
}

// Common presets.
var (
	All           = Opts{StripComments: true, StripInvisible: true}
	InvisibleOnly = Opts{StripComments: false, StripInvisible: true}
	None          = Opts{StripComments: false, StripInvisible: false}
)

// Any reports whether any pass is enabled.
func (o Opts) Any() bool { return o.StripComments || o.StripInvisible }

// Reference sanitizes reference content (comments + invisible chars per opts).
func Reference(input string, o Opts) string {
	out := input
	if o.StripComments {
		out = stripComments(out)
	}
	if o.StripInvisible {
		out = stripInvisible(out)
	}
	return out
}

// Skill sanitizes skill content: invisible characters only (comments preserved,
// since a skill's own markdown comments are legitimate authoring).
func Skill(input string, o Opts) string {
	if o.StripInvisible {
		return stripInvisible(input)
	}
	return input
}

// SkillDir rewrites every markdown file under dir with Skill sanitization.
func SkillDir(dir string, o Opts) error {
	if !o.StripInvisible {
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isMarkdown(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		cleaned := Skill(string(data), o)
		if cleaned != string(data) {
			info, _ := d.Info()
			mode := os.FileMode(0o644)
			if info != nil {
				mode = info.Mode().Perm()
			}
			return os.WriteFile(path, []byte(cleaned), mode)
		}
		return nil
	})
}

func isMarkdown(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".md") || strings.HasSuffix(l, ".markdown")
}

// ---- invisible-char stripping ----------------------------------------------

func stripInvisible(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	first := true
	for _, r := range s {
		if !isInvisible(r, first) {
			b.WriteRune(r)
		}
		first = false
	}
	return b.String()
}

func isInvisible(cp rune, isLeading bool) bool {
	switch {
	case cp == 0x200b || cp == 0x200c || cp == 0x200d: // zero-width space/non-joiner/joiner
		return true
	case cp == 0xfeff && !isLeading: // BOM ok only as the very first codepoint
		return true
	case (cp >= 0x202a && cp <= 0x202e) || (cp >= 0x2066 && cp <= 0x2069): // bidi overrides + isolates
		return true
	case cp >= 0xe0000 && cp <= 0xe007f: // Unicode Tag block
		return true
	default:
		return false
	}
}

// ---- markdown-comment stripping (outside fenced code blocks) ----------------

type commentState struct{ inHTMLComment bool }

func stripComments(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	inFence := false
	st := &commentState{}

	for _, line := range splitInclusive(s) {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			st.inHTMLComment = false
			out.WriteString(line)
			continue
		}
		if inFence {
			out.WriteString(line)
			continue
		}
		out.WriteString(processLine(line, st))
	}
	return out.String()
}

func processLine(line string, st *commentState) string {
	stripped := stripHTMLCommentsOnLine(line, st)
	if isLinkFormCommentLine(stripped) {
		return ""
	}
	return stripped
}

func stripHTMLCommentsOnLine(line string, st *commentState) string {
	chars := []rune(line)
	n := len(chars)
	var out strings.Builder
	i := 0
	for i < n {
		if st.inHTMLComment {
			if i+2 <= n-1 && chars[i] == '-' && chars[i+1] == '-' && chars[i+2] == '>' {
				st.inHTMLComment = false
				i += 3
				continue
			}
			i++
			continue
		}
		if i+3 <= n-1 && chars[i] == '<' && chars[i+1] == '!' && chars[i+2] == '-' && chars[i+3] == '-' {
			if closedAt := indexClose(chars, i+4); closedAt != -1 {
				i = closedAt + 3
				continue
			}
			// Unterminated: the rest of the line is inside the comment.
			st.inHTMLComment = true
			if strings.HasSuffix(line, "\n") {
				out.WriteByte('\n')
			}
			return out.String()
		}
		out.WriteRune(chars[i])
		i++
	}
	return out.String()
}

// indexClose returns the index of the next "-->" at or after `from`, or -1.
func indexClose(chars []rune, from int) int {
	for j := from; j+2 <= len(chars)-1; j++ {
		if chars[j] == '-' && chars[j+1] == '-' && chars[j+2] == '>' {
			return j
		}
	}
	return -1
}

func isLinkFormCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[//]:") {
		return false
	}
	rest := strings.TrimLeft(trimmed[5:], " \t")
	if !strings.HasPrefix(rest, "#") {
		return false
	}
	rest = strings.TrimLeft(rest[1:], " \t")
	if rest == "" {
		return true
	}
	switch rest[0] {
	case '"', '\'', '(':
		return true
	default:
		return false
	}
}

// splitInclusive splits s into lines, each retaining its trailing '\n'.
func splitInclusive(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
