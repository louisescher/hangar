// Package discover crawls an extracted repository (or npm package) tree for
// Agent Skills (SKILL.md) and collects reference documents.
package discover

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// skillFile is the conventional skill manifest filename.
const skillFile = "SKILL.md"

// maxDepth bounds the recursive whole-tree fallback scan.
const maxDepth = 5

// Skill is a discovered skill (or, when IsRef is true, a reference document).
type Skill struct {
	Name        string
	Description string
	RelPath     string // directory path relative to the crawl root, e.g. "document-skills/pdf"
	AbsPath     string // on-disk directory containing the skill
	IsRef       bool

	// FrontmatterRaw is the verbatim YAML frontmatter block (without the ---
	// fences), preserved for display in the preview pane.
	FrontmatterRaw string
}

// DiscoverSkills finds skills under root using rosie's precedence:
//  1. a SKILL.md at the root → that single skill;
//  2. otherwise SKILL.md files under root/skills (recursively);
//  3. otherwise SKILL.md files anywhere in the tree (bounded depth).
//
// Dot-directories are skipped. RelPath values are relative to root.
func DiscoverSkills(root string) ([]Skill, error) {
	if fileExists(filepath.Join(root, skillFile)) {
		s, err := loadSkill(root, root)
		if err != nil {
			return nil, err
		}
		return []Skill{s}, nil
	}

	if dirExists(filepath.Join(root, "skills")) {
		found, err := scanTree(filepath.Join(root, "skills"), root)
		if err != nil {
			return nil, err
		}
		if len(found) > 0 {
			return found, nil
		}
	}

	return scanTree(root, root)
}

// scanTree walks start, collecting one Skill per SKILL.md found. RelPath values
// are computed relative to relRoot. Dot-directories are skipped and recursion is
// bounded by maxDepth (measured from start).
func scanTree(start, relRoot string) ([]Skill, error) {
	var out []Skill
	err := filepath.WalkDir(start, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != start && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if depthBetween(start, path) > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != skillFile {
			return nil
		}
		skillDir := filepath.Dir(path)
		s, err := loadSkill(relRoot, skillDir)
		if err != nil {
			return err
		}
		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

func depthBetween(base, path string) int {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// loadSkill reads and parses skillDir/SKILL.md.
func loadSkill(relRoot, skillDir string) (Skill, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, skillFile))
	if err != nil {
		return Skill{}, err
	}
	fm, _ := SplitFrontmatter(data)
	meta := parseMeta(fm)

	rel, err := filepath.Rel(relRoot, skillDir)
	if err != nil {
		return Skill{}, err
	}
	if rel == "." {
		rel = ""
	}

	name := meta["name"]
	if name == "" {
		name = filepath.Base(skillDir)
	}

	return Skill{
		Name:           name,
		Description:    meta["description"],
		RelPath:        filepath.ToSlash(rel),
		AbsPath:        skillDir,
		FrontmatterRaw: strings.TrimSpace(string(fm)),
	}, nil
}

// SplitFrontmatter returns the YAML frontmatter block (without the --- fences)
// and the remaining markdown body. If the content has no leading frontmatter,
// the frontmatter is empty and body is the whole input.
func SplitFrontmatter(data []byte) (frontmatter, body []byte) {
	const fence = "---"
	// Normalize a possible UTF-8 BOM.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !bytes.HasPrefix(data, []byte(fence)) {
		return nil, data
	}
	// Find the start of the line after the opening fence.
	rest := data[len(fence):]
	if len(rest) == 0 || (rest[0] != '\n' && rest[0] != '\r') {
		return nil, data // "---something" is not a fence
	}
	nl := bytes.IndexByte(rest, '\n')
	if nl < 0 {
		return nil, data
	}
	rest = rest[nl+1:]

	// Find the closing fence at the start of a line.
	idx := indexClosingFence(rest)
	if idx < 0 {
		return nil, data
	}
	fmBlock := rest[:idx]
	after := rest[idx:]
	// Skip the closing fence line.
	if nl := bytes.IndexByte(after, '\n'); nl >= 0 {
		after = after[nl+1:]
	} else {
		after = nil
	}
	return fmBlock, after
}

func indexClosingFence(b []byte) int {
	lineStart := 0
	for lineStart <= len(b) {
		end := bytes.IndexByte(b[lineStart:], '\n')
		var line []byte
		if end < 0 {
			line = b[lineStart:]
		} else {
			line = b[lineStart : lineStart+end]
		}
		if strings.TrimRight(string(line), "\r") == "---" {
			return lineStart
		}
		if end < 0 {
			return -1
		}
		lineStart += end + 1
	}
	return -1
}

// parseMeta extracts a flat string map from the frontmatter, used for name and
// description. Non-scalar values are ignored.
func parseMeta(fm []byte) map[string]string {
	out := map[string]string{}
	if len(fm) == 0 {
		return out
	}
	var raw map[string]any
	if err := yaml.Unmarshal(fm, &raw); err != nil {
		return out
	}
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			out[strings.ToLower(k)] = strings.TrimSpace(val)
		case int, int64, float64, bool:
			out[strings.ToLower(k)] = fmt.Sprintf("%v", val)
		}
	}
	return out
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
