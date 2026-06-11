// Package local materializes skills from a local filesystem path. It satisfies
// fetch.Fetcher; there is nothing to download or clean up.
package local

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/fsutil"
	"github.com/louisescher/hangar/internal/spec"
)

type Fetcher struct{}

func New() *Fetcher { return &Fetcher{} }

func (Fetcher) Resolve(_ context.Context, _ spec.SourceSpec) (ref, sha string, isTag bool, err error) {
	return "", "", false, nil
}

func (Fetcher) Fetch(_ context.Context, s spec.SourceSpec) (fetch.Result, error) {
	p := s.Path
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fetch.Result{}, err
		}
		p = abs
	}
	if !fsutil.IsDir(p) {
		return fetch.Result{}, fmt.Errorf("local source %q is not a directory", p)
	}
	return fetch.Result{Root: p, Cleanup: fetch.NoopCleanup}, nil
}
