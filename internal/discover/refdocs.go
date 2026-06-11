package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// refMaxDepth bounds the recursive walk when collecting reference docs.
const refMaxDepth = 16

// RefDoc is a single reference document discovered within a source tree.
type RefDoc struct {
	RelPath string // path relative to the crawl root, e.g. "docs/hooks.md"
	AbsPath string
}

// CollectRefDocs gathers markdown reference documents from root.
//
// With no include list, the default scope is the root README.md (case
// insensitive) plus every .md file under docs/. With an include list, each
// entry is resolved relative to root: a file is taken verbatim, a directory is
// walked for .md files. Nested node_modules directories are always skipped.
func CollectRefDocs(root string, include []string) ([]RefDoc, error) {
	seen := map[string]bool{}
	var out []RefDoc
	add := func(abs string) {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		if seen[rel] {
			return
		}
		seen[rel] = true
		out = append(out, RefDoc{RelPath: rel, AbsPath: abs})
	}

	if len(include) == 0 {
		if readme := findReadme(root); readme != "" {
			add(readme)
		}
		docsDir := filepath.Join(root, "docs")
		if dirExists(docsDir) {
			if err := walkMarkdown(docsDir, add); err != nil {
				return nil, err
			}
		}
	} else {
		for _, inc := range include {
			p := filepath.Join(root, filepath.FromSlash(inc))
			fi, err := os.Stat(p)
			if err != nil {
				return nil, err
			}
			if fi.IsDir() {
				if err := walkMarkdown(p, add); err != nil {
					return nil, err
				}
			} else {
				add(p)
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

func findReadme(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "README.md") {
			return filepath.Join(root, e.Name())
		}
	}
	// Fall back to any readme* file.
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "readme") {
			return filepath.Join(root, e.Name())
		}
	}
	return ""
}

func walkMarkdown(dir string, add func(abs string)) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if depthBetween(dir, path) > refMaxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			add(path)
		}
		return nil
	})
}
