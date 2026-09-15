// Package github fetches skills from github.com. It is a thin wrapper over the
// generic gitforge fetcher configured with the GitHub profile (github.com refs,
// codeload.github.com tarballs, x-access-token auth).
package github

import (
	"github.com/louisescher/hangar/internal/fetch/gitforge"
	"github.com/louisescher/hangar/internal/httpx"
)

// New returns a gitforge.Client configured for github.com with the given
// (possibly empty) token. If doer is nil, httpx.Default is used.
func New(doer httpx.Doer, token string) *gitforge.Client {
	return gitforge.New(doer, gitforge.GitHubProfile(), token)
}
