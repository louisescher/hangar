package discover

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/louisescher/hangar/internal/fsutil"
)

// Inline markdown links / images: capture the target inside (...). Also handles
// reference-style definitions ([id]: target).
var (
	inlineLinkRe = regexp.MustCompile(`!?\[[^\]]*\]\(\s*([^)\s]+)`)
	refDefRe     = regexp.MustCompile(`(?m)^\s*\[[^\]]+\]:\s+(\S+)`)
)

// ResolveSkillFiles returns the repo-relative paths of files referenced by a
// skill's markdown that live inside repoRoot but outside the skill directory —
// the files that must be vendored alongside the skill for it to work
// standalone. It follows relative links transitively through markdown files up
// to maxDepth. External URLs, anchors, and absolute paths are ignored, and
// nothing outside repoRoot is ever returned.
func ResolveSkillFiles(repoRoot, skillDir string, maxDepth int) ([]string, error) {
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	absSkill, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, err
	}

	type item struct {
		file  string
		depth int
	}
	visited := map[string]bool{}
	vendored := map[string]bool{}

	queue := []item{{filepath.Join(absSkill, skillFile), 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur.file] {
			continue
		}
		visited[cur.file] = true

		data, err := os.ReadFile(cur.file)
		if err != nil {
			continue
		}

		for _, target := range extractLinkTargets(data) {
			target = cleanTarget(target)
			if target == "" || isExternal(target) || filepath.IsAbs(target) {
				continue
			}
			abs := filepath.Clean(filepath.Join(filepath.Dir(cur.file), target))

			if ok, _ := fsutil.WithinRoot(absRepo, abs); !ok {
				continue
			}
			fi, err := os.Stat(abs)
			if err != nil || fi.IsDir() {
				continue
			}

			if ok, _ := fsutil.WithinRoot(absSkill, abs); !ok {
				rel, err := filepath.Rel(absRepo, abs)
				if err == nil {
					vendored[filepath.ToSlash(rel)] = true
				}
			}

			// Follow markdown references transitively (inside or outside the skill).
			if strings.EqualFold(filepath.Ext(abs), ".md") && cur.depth < maxDepth {
				queue = append(queue, item{abs, cur.depth + 1})
			}
		}
	}

	out := make([]string, 0, len(vendored))
	for p := range vendored {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func extractLinkTargets(data []byte) []string {
	var out []string
	for _, m := range inlineLinkRe.FindAllSubmatch(data, -1) {
		out = append(out, string(m[1]))
	}
	for _, m := range refDefRe.FindAllSubmatch(data, -1) {
		out = append(out, string(m[1]))
	}
	return out
}

// cleanTarget strips a markdown title (`path "title"`) and any #fragment.
func cleanTarget(t string) string {
	t = strings.TrimSpace(t)
	t = strings.Trim(t, "<>")
	if i := strings.IndexAny(t, " \t"); i >= 0 {
		t = t[:i]
	}
	if i := strings.IndexByte(t, '#'); i >= 0 {
		t = t[:i]
	}
	return t
}

func isExternal(t string) bool {
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
		return true
	}
	if strings.Contains(t, "://") {
		return true
	}
	for _, scheme := range []string{"mailto:", "tel:", "data:"} {
		if strings.HasPrefix(t, scheme) {
			return true
		}
	}
	return false
}
