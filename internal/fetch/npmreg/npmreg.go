// Package npmreg fetches skills and reference docs from npm packages. It reads
// the registry packument, resolves a version (latest, a dist-tag, or a pinned
// exact version), downloads the dist tarball, verifies its integrity, and
// extracts it. It satisfies fetch.Fetcher.
//
// Auth and registry selection come from the resolved .npmrc configuration
// (config.NPMRC), so scoped and private registries work transparently. Semver
// range resolution is intentionally out of scope: a pinned ref must be an exact
// version or a dist-tag.
package npmreg

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/louisescher/hangar/internal/archive"
	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/fsutil"
	"github.com/louisescher/hangar/internal/httpx"
	"github.com/louisescher/hangar/internal/spec"
)

// maxTarballBytes caps an in-memory tarball download. Packages with skills and
// docs are small; this is a generous ceiling that also guards integrity hashing.
const maxTarballBytes = 256 << 20

// Client fetches from an npm registry.
type Client struct {
	HTTP httpx.Doer
	RC   *config.NPMRC
}

// New returns a Client. A nil doer uses httpx.Default; a nil rc uses the
// built-in defaults (public registry, no auth).
func New(doer httpx.Doer, rc *config.NPMRC) *Client {
	if doer == nil {
		doer = httpx.Default
	}
	if rc == nil {
		rc = config.ParseNPMRC("")
	}
	return &Client{HTTP: doer, RC: rc}
}

// distInfo is the subset of a version's "dist" object we need.
type distInfo struct {
	Tarball   string `json:"tarball"`
	Integrity string `json:"integrity"`
	Shasum    string `json:"shasum"`
}

// packument is the subset of a registry packument document we need.
type packument struct {
	DistTags map[string]string `json:"dist-tags"`
	Versions map[string]struct {
		Version string   `json:"version"`
		Dist    distInfo `json:"dist"`
	} `json:"versions"`
}

// Resolve implements fetch.Fetcher. For npm, ref is the resolved version and
// sha is empty (versions are immutable, so there is no tag-rewrite analogue).
func (c *Client) Resolve(ctx context.Context, s spec.SourceSpec) (ref, sha string, isTag bool, err error) {
	pm, err := c.packument(ctx, s.Pkg)
	if err != nil {
		return "", "", false, err
	}
	version, _, err := resolveVersion(pm, s.Ref, s.Pkg)
	if err != nil {
		return "", "", false, err
	}
	return version, "", false, nil
}

// Fetch implements fetch.Fetcher: it downloads, verifies, and extracts the
// resolved version's tarball, rooting at any spec subpath.
func (c *Client) Fetch(ctx context.Context, s spec.SourceSpec) (fetch.Result, error) {
	pm, err := c.packument(ctx, s.Pkg)
	if err != nil {
		return fetch.Result{}, err
	}
	version, dist, err := resolveVersion(pm, s.Ref, s.Pkg)
	if err != nil {
		return fetch.Result{}, err
	}
	if dist.Tarball == "" {
		return fetch.Result{}, fmt.Errorf("%s@%s: no tarball in registry metadata", s.Pkg, version)
	}

	data, err := c.download(ctx, dist.Tarball)
	if err != nil {
		return fetch.Result{}, err
	}
	if err := verifyIntegrity(data, dist.Integrity, dist.Shasum); err != nil {
		return fetch.Result{}, fmt.Errorf("%s@%s: %w", s.Pkg, version, err)
	}

	tmp, err := os.MkdirTemp("", "hangar-npm-")
	if err != nil {
		return fetch.Result{}, err
	}
	cleanup := func() error { return os.RemoveAll(tmp) }

	if err := archive.ExtractTarGz(bytes.NewReader(data), tmp); err != nil {
		_ = cleanup()
		return fetch.Result{}, err
	}
	// npm tarballs wrap everything in a single top-level "package/" directory.
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
			return fetch.Result{}, fmt.Errorf("subpath %q not found in %s@%s", s.Subpath, s.Pkg, version)
		}
		root = sub
	}

	return fetch.Result{Root: root, Ref: version, IsTag: false, Cleanup: cleanup}, nil
}

// resolveVersion picks a concrete version: the "latest" dist-tag when ref is
// empty, the dist-tag's target when ref names one, or the exact version when
// ref is a version present in the packument.
func resolveVersion(pm *packument, ref, pkg string) (string, distInfo, error) {
	if ref == "" {
		ref = "latest"
	}
	if v, ok := pm.DistTags[ref]; ok {
		ref = v
	}
	vi, ok := pm.Versions[ref]
	if !ok {
		return "", distInfo{}, fmt.Errorf("%s: version or dist-tag %q not found (semver ranges are not supported; pin an exact version)", pkg, ref)
	}
	version := vi.Version
	if version == "" {
		version = ref
	}
	return version, vi.Dist, nil
}

func (c *Client) packument(ctx context.Context, pkg string) (*packument, error) {
	registry := c.RC.RegistryFor(pkg)
	url := joinURL(registry, packagePath(pkg))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The install-v1 abbreviated document is smaller and sufficient here.
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*")
	c.auth(req, url)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("npm package %q not found in registry %s", pkg, registry)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("npm registry %s: package %q: HTTP %d", registry, pkg, resp.StatusCode)
	}
	var pm packument
	if err := json.NewDecoder(resp.Body).Decode(&pm); err != nil {
		return nil, fmt.Errorf("decode packument for %q: %w", pkg, err)
	}
	return &pm, nil
}

func (c *Client) download(ctx context.Context, tarballURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tarballURL, nil)
	if err != nil {
		return nil, err
	}
	c.auth(req, tarballURL)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download tarball %s: HTTP %d", tarballURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTarballBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTarballBytes {
		return nil, fmt.Errorf("tarball %s exceeds %d bytes", tarballURL, maxTarballBytes)
	}
	return data, nil
}

// auth attaches a Bearer token when one is configured for the request URL's
// registry.
func (c *Client) auth(req *http.Request, url string) {
	if tok := c.RC.AuthToken(url); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
}

// verifyIntegrity checks the tarball bytes against the registry's Subresource
// Integrity hash (dist.integrity) or, failing that, the legacy sha1 dist.shasum.
// When the registry advertises neither, the download is accepted.
func verifyIntegrity(data []byte, integrity, shasum string) error {
	if integrity != "" {
		// integrity may list several space-separated "<algo>-<base64>" hashes.
		for _, entry := range strings.Fields(integrity) {
			algo, b64, ok := strings.Cut(entry, "-")
			if !ok {
				continue
			}
			want, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				continue
			}
			var sum []byte
			switch algo {
			case "sha512":
				h := sha512.Sum512(data)
				sum = h[:]
			case "sha256":
				h := sha256.Sum256(data)
				sum = h[:]
			case "sha1":
				h := sha1.Sum(data)
				sum = h[:]
			default:
				continue // unknown algorithm: try the next entry
			}
			if bytes.Equal(sum, want) {
				return nil
			}
			return fmt.Errorf("integrity mismatch (%s)", algo)
		}
	}
	if shasum != "" {
		h := sha1.Sum(data)
		if !strings.EqualFold(hex.EncodeToString(h[:]), shasum) {
			return fmt.Errorf("shasum mismatch")
		}
		return nil
	}
	return nil
}

// packagePath URL-encodes a scoped package's slash so it forms a single path
// segment, as the registry expects (@scope%2fname).
func packagePath(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		return strings.Replace(pkg, "/", "%2f", 1)
	}
	return pkg
}

func joinURL(base, p string) string {
	return strings.TrimRight(base, "/") + "/" + p
}
