// Package github fetches skills from GitHub repositories: it resolves a ref via
// the git smart-HTTP advertisement, downloads the codeload tarball, and
// extracts it. It satisfies fetch.Fetcher.
package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/louisescher/hangar/internal/archive"
	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/fsutil"
	"github.com/louisescher/hangar/internal/httpx"
	"github.com/louisescher/hangar/internal/spec"
)

// Client fetches from GitHub. The URL fields are overridable for testing.
type Client struct {
	HTTP        httpx.Doer
	Token       string
	BaseURL     string // git host, default https://github.com
	CodeloadURL string // tarball host, default https://codeload.github.com
}

// New returns a Client with default endpoints and the given (possibly empty)
// token. If doer is nil, httpx.Default is used.
func New(doer httpx.Doer, token string) *Client {
	if doer == nil {
		doer = httpx.Default
	}
	return &Client{
		HTTP:        doer,
		Token:       token,
		BaseURL:     "https://github.com",
		CodeloadURL: "https://codeload.github.com",
	}
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// refs holds the parsed advertisement.
type refs struct {
	tags  map[string]string // tag name -> commit SHA (peeled when annotated)
	heads map[string]string // branch name -> commit SHA
}

// Resolve implements fetch.Fetcher.
func (c *Client) Resolve(ctx context.Context, s spec.SourceSpec) (ref, sha string, isTag bool, err error) {
	r, err := c.lsRefs(ctx, s.Owner, s.Repo)
	if err != nil {
		return "", "", false, err
	}

	if s.Ref != "" {
		if sha, ok := r.tags[s.Ref]; ok {
			return s.Ref, sha, true, nil
		}
		if sha, ok := r.heads[s.Ref]; ok {
			return s.Ref, sha, false, nil
		}
		// Unknown ref name: assume a commit SHA (or a ref codeload can resolve).
		if shaRe.MatchString(s.Ref) {
			return s.Ref, s.Ref, false, nil
		}
		return s.Ref, "", false, nil
	}

	if tag, sha, ok := selectLatestTag(r.tags, s.Repo); ok {
		return tag, sha, true, nil
	}
	for _, b := range []string{"main", "master"} {
		if sha, ok := r.heads[b]; ok {
			return b, sha, false, nil
		}
	}
	return "", "", false, fmt.Errorf("%s/%s: no semver tag and no main/master branch", s.Owner, s.Repo)
}

// Fetch implements fetch.Fetcher.
func (c *Client) Fetch(ctx context.Context, s spec.SourceSpec) (fetch.Result, error) {
	ref, sha, isTag, err := c.Resolve(ctx, s)
	if err != nil {
		return fetch.Result{}, err
	}

	downloadRef := sha
	if downloadRef == "" {
		downloadRef = ref
	}
	if downloadRef == "" {
		return fetch.Result{}, fmt.Errorf("%s/%s: could not determine a ref to download", s.Owner, s.Repo)
	}

	tmp, err := os.MkdirTemp("", "hangar-gh-")
	if err != nil {
		return fetch.Result{}, err
	}
	cleanup := func() error { return os.RemoveAll(tmp) }

	rc, err := c.downloadTarball(ctx, s.Owner, s.Repo, downloadRef)
	if err != nil {
		_ = cleanup()
		return fetch.Result{}, err
	}
	extractErr := archive.ExtractTarGz(rc, tmp)
	rc.Close()
	if extractErr != nil {
		_ = cleanup()
		return fetch.Result{}, extractErr
	}

	root, err := archive.SingleRoot(tmp)
	if err != nil {
		_ = cleanup()
		return fetch.Result{}, err
	}

	if s.Subpath != "" {
		sub := filepath.Join(root, filepath.FromSlash(s.Subpath))
		if err := fsutil.MustWithinRoot(root, sub); err != nil {
			_ = cleanup()
			return fetch.Result{}, err
		}
		if !fsutil.IsDir(sub) {
			_ = cleanup()
			return fetch.Result{}, fmt.Errorf("subpath %q no longer exists in %s/%s: %w", s.Subpath, s.Owner, s.Repo, fetch.ErrNotFound)
		}
		root = sub
	}

	return fetch.Result{Root: root, Ref: ref, SHA: sha, IsTag: isTag, Cleanup: cleanup}, nil
}

func (c *Client) lsRefs(ctx context.Context, owner, repo string) (refs, error) {
	url := fmt.Sprintf("%s/%s/%s.git/info/refs?service=git-upload-pack", c.BaseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return refs{}, err
	}
	c.auth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return refs{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		return refs{}, fmt.Errorf("repository %s/%s not found (it may be private or misspelled; set GH_TOKEN for private repos): %w", owner, repo, fetch.ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return refs{}, fmt.Errorf("could not reach %s/%s: GitHub returned HTTP %d", owner, repo, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return refs{}, err
	}
	return parseAdvertisement(body)
}

func (c *Client) downloadTarball(ctx context.Context, owner, repo, ref string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/%s/%s/tar.gz/%s", c.CodeloadURL, owner, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s/%s@%s: HTTP %d", owner, repo, ref, resp.StatusCode)
	}
	return resp.Body, nil
}

// auth adds HTTP Basic credentials (x-access-token:<token>) when a token is set.
func (c *Client) auth(req *http.Request) {
	if c.Token == "" {
		return
	}
	cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + c.Token))
	req.Header.Set("Authorization", "Basic "+cred)
}

// parseAdvertisement parses a git-upload-pack smart-HTTP advertisement into the
// set of tag and branch refs.
func parseAdvertisement(data []byte) (refs, error) {
	lines, err := parsePktLines(data)
	if err != nil {
		return refs{}, err
	}
	r := refs{tags: map[string]string{}, heads: map[string]string{}}
	for _, ln := range lines {
		s := strings.TrimRight(string(ln), "\n")
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		// Strip capabilities advertised after the first ref (sha ref\x00caps).
		if i := strings.IndexByte(s, 0); i >= 0 {
			s = s[:i]
		}
		sp := strings.SplitN(s, " ", 2)
		if len(sp) != 2 {
			continue
		}
		sha, name := sp[0], sp[1]
		switch {
		case strings.HasPrefix(name, "refs/tags/"):
			tag := strings.TrimPrefix(name, "refs/tags/")
			peeled := strings.HasSuffix(tag, "^{}")
			tag = strings.TrimSuffix(tag, "^{}")
			// Prefer the peeled commit SHA for annotated tags.
			if peeled || r.tags[tag] == "" {
				r.tags[tag] = sha
			}
		case strings.HasPrefix(name, "refs/heads/"):
			r.heads[strings.TrimPrefix(name, "refs/heads/")] = sha
		}
	}
	return r, nil
}

// parsePktLines splits a git pkt-line stream into payloads, dropping flush
// packets ("0000").
func parsePktLines(data []byte) ([][]byte, error) {
	var out [][]byte
	for len(data) >= 4 {
		n, err := strconv.ParseUint(string(data[:4]), 16, 32)
		if err != nil {
			return nil, fmt.Errorf("pkt-line length: %w", err)
		}
		if n == 0 { // flush packet
			data = data[4:]
			continue
		}
		if int(n) > len(data) || n < 4 {
			return nil, fmt.Errorf("pkt-line length %d out of range", n)
		}
		out = append(out, data[4:n])
		data = data[n:]
	}
	return out, nil
}
