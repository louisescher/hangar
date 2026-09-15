package gitforge

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/httpx"
	"github.com/louisescher/hangar/internal/spec"
)

// TokenSource resolves an access token for a forge dialect and bare host.
// config.Forges satisfies it.
type TokenSource interface {
	TokenFor(forge, host string) string
}

// Router is a fetch.Fetcher for KindGit sources. It builds the right Client per
// (forge, host) from the spec, resolving the token via a TokenSource, and
// caches clients across calls.
type Router struct {
	http   httpx.Doer
	tokens TokenSource

	mu    sync.Mutex
	cache map[string]*Client
}

// NewRouter returns a Router. If doer is nil, httpx.Default is used. tokens may
// be nil (all access is unauthenticated).
func NewRouter(doer httpx.Doer, tokens TokenSource) *Router {
	return &Router{http: doer, tokens: tokens, cache: map[string]*Client{}}
}

// Resolve implements fetch.Fetcher.
func (r *Router) Resolve(ctx context.Context, s spec.SourceSpec) (ref, sha string, isTag bool, err error) {
	c, err := r.clientFor(s)
	if err != nil {
		return "", "", false, err
	}
	return c.Resolve(ctx, s)
}

// Fetch implements fetch.Fetcher.
func (r *Router) Fetch(ctx context.Context, s spec.SourceSpec) (fetch.Result, error) {
	c, err := r.clientFor(s)
	if err != nil {
		return fetch.Result{}, err
	}
	return c.Fetch(ctx, s)
}

func (r *Router) clientFor(s spec.SourceSpec) (*Client, error) {
	if s.Host == "" {
		return nil, fmt.Errorf("git source missing host")
	}
	key := string(s.Forge) + "\x00" + s.Host
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[key]; ok {
		return c, nil
	}
	p, err := ProfileFor(s.Forge, s.Host)
	if err != nil {
		return nil, err
	}
	var token string
	if r.tokens != nil {
		token = r.tokens.TokenFor(string(s.Forge), bareHost(s.Host))
	}
	c := New(r.http, p, token)
	r.cache[key] = c
	return c, nil
}

// bareHost strips the scheme from a host origin ("https://gitlab.com" ->
// "gitlab.com") for token lookup.
func bareHost(host string) string {
	for _, sc := range []string{"https://", "http://"} {
		if strings.HasPrefix(host, sc) {
			return strings.TrimPrefix(host, sc)
		}
	}
	return host
}
