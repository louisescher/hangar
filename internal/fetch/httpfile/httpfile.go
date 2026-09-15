package httpfile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/louisescher/hangar/internal/discover"
	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/httpx"
	"github.com/louisescher/hangar/internal/spec"
)

type Fetcher struct {
	HTTP httpx.Doer
}

func New(doer httpx.Doer) *Fetcher {
	if doer == nil {
		doer = httpx.Default
	}
	return &Fetcher{HTTP: doer}
}

func (f *Fetcher) Resolve(ctx context.Context, s spec.SourceSpec) (ref, sha string, isTag bool, err error) {
	_, sha, err = f.download(ctx, s.URL)
	if err != nil {
		return "", "", false, err
	}
	return "", sha, false, nil
}

func (f *Fetcher) Fetch(ctx context.Context, s spec.SourceSpec) (fetch.Result, error) {
	data, sha, err := f.download(ctx, s.URL)
	if err != nil {
		return fetch.Result{}, err
	}
	tmp, err := os.MkdirTemp("", "hangar-http-")
	if err != nil {
		return fetch.Result{}, err
	}
	cleanup := func() error { return os.RemoveAll(tmp) }
	root := filepath.Join(tmp, slugFor(s.URL))
	if err := os.MkdirAll(root, 0o755); err != nil {
		_ = cleanup()
		return fetch.Result{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), data, 0o644); err != nil {
		_ = cleanup()
		return fetch.Result{}, err
	}
	return fetch.Result{Root: root, SHA: sha, Cleanup: cleanup}, nil
}

func (f *Fetcher) download(ctx context.Context, raw string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", fmt.Errorf("skill at %s is not available: %w", raw, fetch.ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch %s: HTTP %d", raw, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if err := validate(data); err != nil {
		return nil, "", fmt.Errorf("%s: %w", raw, err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

type invalidSkillError struct{ reason string }

func (e invalidSkillError) Error() string { return "not a valid skill file: " + e.reason }

func (invalidSkillError) Is(target error) bool { return target == fetch.ErrNotFound }

func validate(data []byte) error {
	fm, _ := discover.SplitFrontmatter(data)
	if len(fm) == 0 {
		return invalidSkillError{reason: "missing frontmatter"}
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return invalidSkillError{reason: "malformed frontmatter"}
	}
	if strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
		return invalidSkillError{reason: "missing name or description"}
	}
	return nil
}

func slugFor(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "skill"
	}
	seg := path.Base(u.Path)
	for _, ext := range []string{".markdown", ".md"} {
		seg = strings.TrimSuffix(seg, ext)
	}
	seg = strings.ToLower(seg)
	var b strings.Builder
	for _, r := range seg {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "skill"
	}
	return out
}
