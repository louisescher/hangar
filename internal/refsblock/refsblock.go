// Package refsblock maintains a managed block of reference links inside an
// agent-instructions file (AGENTS.md / CLAUDE.md / GEMINI.md /
// .github/copilot-instructions.md).
package refsblock

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/louisescher/hangar/internal/fsutil"
)

const (
	startMarker = "<!-- hangar:references:start -->"
	endMarker   = "<!-- hangar:references:end -->"
)

// candidates lists target instruction files in precedence order. The first that
// already exists is used; otherwise the block is written to AGENTS.md.
var candidates = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"GEMINI.md",
	filepath.Join(".github", "copilot-instructions.md"),
}

// Link is one reference entry rendered into the block.
type Link struct {
	Title string // display text (the reference's H1, or its name)
	Path  string // path used in the markdown link, e.g. ./.agents/references/x/REFERENCE.md
}

// TargetPath returns the instruction file to manage under baseDir: the first
// existing candidate, or AGENTS.md when none exist.
func TargetPath(baseDir string) string {
	for _, c := range candidates {
		if fsutil.Exists(filepath.Join(baseDir, c)) {
			return filepath.Join(baseDir, c)
		}
	}
	return filepath.Join(baseDir, candidates[0])
}

// Rebuild rewrites the managed references block in the target instruction file
// to contain exactly links. When links is empty the block is removed. It
// returns the target file's path relative to baseDir, or "" when nothing was
// written (no block existed and there were no links to add).
func Rebuild(baseDir string, links []Link) (string, error) {
	target := TargetPath(baseDir)
	existing, _ := os.ReadFile(target) // missing file => empty content

	body := buildBody(links)
	updated, changed := replaceBlock(string(existing), body)
	if !changed {
		return "", nil
	}
	if err := fsutil.AtomicWriteFile(target, []byte(updated), 0o644); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(baseDir, target)
	if err != nil {
		rel = target
	}
	return rel, nil
}

// buildBody renders the inner block content (without the markers). Returns ""
// when there are no links.
func buildBody(links []Link) string {
	if len(links) == 0 {
		return ""
	}
	sorted := append([]Link(nil), links...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Title < sorted[j].Title })

	var b strings.Builder
	b.WriteString("<references>\n")
	for _, l := range sorted {
		fmt.Fprintf(&b, "- [%s](%s)\n", l.Title, l.Path)
	}
	b.WriteString("</references>")
	return b.String()
}

// replaceBlock returns content with its managed block set to body (wrapped in
// markers), and whether the content changed. An empty body removes the block.
func replaceBlock(content, body string) (string, bool) {
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)

	// No existing block.
	if start == -1 || end == -1 || end < start {
		if body == "" {
			return content, false
		}
		block := startMarker + "\n" + body + "\n" + endMarker
		if content == "" {
			return block + "\n", true
		}
		sep := "\n"
		if !strings.HasSuffix(content, "\n") {
			sep = "\n\n"
		} else if !strings.HasSuffix(content, "\n\n") {
			sep = "\n"
		}
		return content + sep + block + "\n", true
	}

	// Replace (or remove) the existing block.
	prefix := content[:start]
	suffix := content[end+len(endMarker):]
	if body == "" {
		// Remove the block and collapse surrounding blank lines.
		merged := strings.TrimRight(prefix, "\n")
		rest := strings.TrimLeft(suffix, "\n")
		var out string
		switch {
		case merged == "":
			out = rest
		case rest == "":
			out = merged + "\n"
		default:
			out = merged + "\n\n" + rest
		}
		if !strings.HasSuffix(out, "\n") && out != "" {
			out += "\n"
		}
		return out, out != content
	}
	block := startMarker + "\n" + body + "\n" + endMarker
	out := prefix + block + suffix
	return out, out != content
}

// ExtractTitle returns the first markdown H1 (`# Title`) in data, cleaned of
// inline markdown (badges, links) so it renders safely inside a `[title](path)`
// link. Falls back to fallback when no H1 is present or it cleans to empty.
func ExtractTitle(data []byte, fallback string) string {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inFrontmatter := false
	first := true
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if first {
			first = false
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
				continue
			}
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(line, "# ") {
			if title := cleanInlineMarkdown(line[2:]); title != "" {
				return title
			}
			return fallback
		}
	}
	return fallback
}

var (
	mdImageRe = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	wsRe      = regexp.MustCompile(`\s+`)
)

// cleanInlineMarkdown reduces a heading to plain text: it drops inline images
// (badges), unwraps links to their text, collapses whitespace, and trims. This
// keeps a title usable as the visible text of an outer markdown link.
func cleanInlineMarkdown(s string) string {
	s = mdImageRe.ReplaceAllString(s, "")  // ![alt](url)        -> ""
	s = mdLinkRe.ReplaceAllString(s, "$1") // [text](url)        -> text
	s = strings.ReplaceAll(s, "]", "")
	s = strings.ReplaceAll(s, "[", "")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
