// Package index ships a small curated catalog of known skill sources, embedded
// into the binary. Each entry's Spec is a source specifier fed straight into
// spec.Parse, so the catalog is just a convenient front door to install.
package index

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

//go:embed catalog.json
var catalogJSON []byte

// Entry is one curated source in the catalog.
type Entry struct {
	Name        string   `json:"name"`
	Spec        string   `json:"spec"` // owner/repo[/sub][@ref] or npm:pkg, etc.
	Description string   `json:"description"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

var (
	once    sync.Once
	entries []Entry
)

// Catalog returns the embedded curated entries, sorted by category then name.
// The slice is shared; callers must not mutate it.
func Catalog() []Entry {
	once.Do(func() {
		_ = json.Unmarshal(catalogJSON, &entries)
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Category != entries[j].Category {
				return entries[i].Category < entries[j].Category
			}
			return entries[i].Name < entries[j].Name
		})
	})
	return entries
}

// Search returns catalog entries matching q (case-insensitive substring over
// name, description, spec, and tags). An empty query returns the whole catalog.
func Search(q string) []Entry {
	all := Catalog()
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return all
	}
	var out []Entry
	for _, e := range all {
		if e.matches(q) {
			out = append(out, e)
		}
	}
	return out
}

func (e Entry) matches(q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) ||
		strings.Contains(strings.ToLower(e.Description), q) ||
		strings.Contains(strings.ToLower(e.Spec), q) ||
		strings.Contains(strings.ToLower(e.Category), q) {
		return true
	}
	for _, t := range e.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}
