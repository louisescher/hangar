package index

import (
	"testing"

	"github.com/louisescher/hangar/internal/spec"
)

func TestCatalogSpecsParse(t *testing.T) {
	// Every curated spec must be a valid source specifier, so the catalog can
	// never ship an entry that fails at install time.
	for _, e := range Catalog() {
		if _, err := spec.Parse(e.Spec); err != nil {
			t.Errorf("catalog entry %q has unparseable spec %q: %v", e.Name, e.Spec, err)
		}
	}
}

func TestCatalogLoads(t *testing.T) {
	all := Catalog()
	if len(all) == 0 {
		t.Fatal("embedded catalog is empty")
	}
	// Entries must be grouped by category (then name) and carry a parseable spec.
	for i, e := range all {
		if e.Name == "" || e.Spec == "" {
			t.Errorf("entry %d missing name/spec: %+v", i, e)
		}
		if i > 0 {
			prev := all[i-1]
			if prev.Category > e.Category || (prev.Category == e.Category && prev.Name > e.Name) {
				t.Errorf("catalog not sorted at %d: (%q,%q) after (%q,%q)", i, e.Category, e.Name, prev.Category, prev.Name)
			}
		}
	}
}

func TestSearch(t *testing.T) {
	if got := Search(""); len(got) != len(Catalog()) {
		t.Errorf("empty query should return all (%d), got %d", len(Catalog()), len(got))
	}
	hits := Search("pdf")
	if len(hits) == 0 {
		t.Fatal("expected a pdf match")
	}
	for _, h := range hits {
		if !h.matches("pdf") {
			t.Errorf("non-matching entry returned: %+v", h)
		}
	}
	if got := Search("definitely-not-in-catalog-xyz"); len(got) != 0 {
		t.Errorf("expected no matches, got %d", len(got))
	}
}
