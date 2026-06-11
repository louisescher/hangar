// Package fetch defines the contract every source backend (GitHub, npm, local)
// implements: resolving a spec to a concrete ref/version and materializing its
// tree on disk. Concrete backends live in subpackages; the dispatch over
// spec.Kind lives in higher layers (engine/install) to avoid an import cycle.
package fetch

import (
	"context"

	"github.com/louisescher/hangar/internal/spec"
)

// Result is the materialized form of a source: an on-disk tree rooted at Root,
// plus the resolved ref/version metadata recorded in the lockfile.
type Result struct {
	// Root is the directory to crawl for skills — the extracted repository root
	// joined with any spec subpath.
	Root string
	// Ref is the human-facing ref (tag/branch for GitHub, version for npm). May
	// be empty for local sources.
	Ref string
	// SHA is the resolved commit SHA (GitHub) or package version (npm). Empty
	// for local sources.
	SHA string
	// IsTag is true when Ref names a tag (relevant to tag-rewrite detection).
	IsTag bool
	// Cleanup releases any temporary extraction directory. Always non-nil.
	Cleanup func() error
}

// noopCleanup is a convenience for sources with nothing to clean up.
func NoopCleanup() error { return nil }

// Fetcher resolves and materializes a single source kind.
type Fetcher interface {
	// Resolve determines the ref/version and SHA without downloading the tree.
	Resolve(ctx context.Context, s spec.SourceSpec) (ref, sha string, isTag bool, err error)
	// Fetch materializes the source tree on disk and returns its Result.
	Fetch(ctx context.Context, s spec.SourceSpec) (Result, error)
}
