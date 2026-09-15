package gitforge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/louisescher/hangar/internal/spec"
)

// pkt encodes a git pkt-line.
func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// advertisement builds a git-upload-pack smart-HTTP advertisement.
func advertisement(refs [][2]string) string {
	var b strings.Builder
	b.WriteString(pkt("# service=git-upload-pack\n"))
	b.WriteString("0000") // flush
	for i, r := range refs {
		line := r[0] + " " + r[1]
		if i == 0 {
			line += "\x00multi_ack thin-pack side-band-64k"
		}
		b.WriteString(pkt(line + "\n"))
	}
	b.WriteString("0000")
	return b.String()
}

// makeTarGz builds a gzipped tar with the given files (paths relative to a
// single top-level root dir).
func makeTarGz(t *testing.T, rootDir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		full := rootDir + "/" + name
		if err := tw.WriteHeader(&tar.Header{Name: full, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// forgeCase describes one provider's profile and the tarball route layout its
// profile produces, so a single test server can serve each dialect.
type forgeCase struct {
	name     string
	profile  func(host string) Profile
	tarRoute string // mux path prefix where this profile's tarballs land
}

var forgeCases = []forgeCase{
	{"github", func(string) Profile { return GitHubProfile() }, "/owner/skills/tar.gz/"},
	{"gitlab", GitLabProfile, "/owner/skills/-/archive/"},
	{"forgejo", ForgejoProfile, "/owner/skills/archive/"},
	{"bitbucket", BitbucketProfile, "/owner/skills/get/"},
	{"tangled", TangledProfile, "/owner/skills/archive/"},
	{"generic", GenericProfile, "/owner/skills/archive/"},
}

// newTestServer serves the ref advertisement and the tarball at the route the
// given profile downloads from. It records the auth header seen on info/refs.
func newTestServer(t *testing.T, c forgeCase, adv string, tarball []byte, seenAuth *http.Header) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/owner/skills.git/info/refs", func(w http.ResponseWriter, r *http.Request) {
		if seenAuth != nil {
			*seenAuth = r.Header.Clone()
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(adv))
	})
	mux.HandleFunc(c.tarRoute, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	})
	return httptest.NewServer(mux)
}

// clientFor builds a Client for a profile pointed at the test server.
func clientFor(srv *httptest.Server, c forgeCase, token string) *Client {
	cl := New(srv.Client(), c.profile(srv.URL), token)
	cl.BaseURL = srv.URL
	cl.TarballBaseURL = srv.URL
	return cl
}

func TestResolveLatestTag(t *testing.T) {
	adv := advertisement([][2]string{
		{"1111111111111111111111111111111111111111", "refs/heads/main"},
		{"2222222222222222222222222222222222222222", "refs/tags/v1.0.0"},
		{"3333333333333333333333333333333333333333", "refs/tags/v1.2.0"},
	})
	for _, c := range forgeCases {
		t.Run(c.name, func(t *testing.T) {
			srv := newTestServer(t, c, adv, nil, nil)
			defer srv.Close()
			cl := clientFor(srv, c, "")

			ref, sha, isTag, err := cl.Resolve(context.Background(), spec.SourceSpec{Owner: "owner", Repo: "skills"})
			if err != nil {
				t.Fatal(err)
			}
			if ref != "v1.2.0" || sha != "3333333333333333333333333333333333333333" || !isTag {
				t.Errorf("got ref=%q sha=%q isTag=%v, want v1.2.0/333.../true", ref, sha, isTag)
			}
		})
	}
}

func TestResolveExplicitBranch(t *testing.T) {
	adv := advertisement([][2]string{
		{"1111111111111111111111111111111111111111", "refs/heads/main"},
		{"3333333333333333333333333333333333333333", "refs/tags/v1.2.0"},
	})
	c := forgeCases[0] // github
	srv := newTestServer(t, c, adv, nil, nil)
	defer srv.Close()
	cl := clientFor(srv, c, "")

	ref, sha, isTag, err := cl.Resolve(context.Background(), spec.SourceSpec{Owner: "owner", Repo: "skills", Ref: "main", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "main" || sha != "1111111111111111111111111111111111111111" || isTag {
		t.Errorf("got ref=%q sha=%q isTag=%v, want main/111.../false", ref, sha, isTag)
	}
}

func TestFetchExtractsWithSubpath(t *testing.T) {
	adv := advertisement([][2]string{
		{"3333333333333333333333333333333333333333", "refs/tags/v1.2.0"},
	})
	tarball := makeTarGz(t, "skills-v1.2.0", map[string]string{
		"README.md":                    "# repo",
		"document-skills/pdf/SKILL.md": "---\nname: pdf\n---\n",
	})
	for _, c := range forgeCases {
		t.Run(c.name, func(t *testing.T) {
			srv := newTestServer(t, c, adv, tarball, nil)
			defer srv.Close()
			cl := clientFor(srv, c, "")

			res, err := cl.Fetch(context.Background(), spec.SourceSpec{Owner: "owner", Repo: "skills", Subpath: "document-skills/pdf"})
			if err != nil {
				t.Fatal(err)
			}
			defer res.Cleanup()

			if res.Ref != "v1.2.0" || !res.IsTag {
				t.Errorf("got ref=%q isTag=%v", res.Ref, res.IsTag)
			}
			if !strings.HasSuffix(filepath.ToSlash(res.Root), "document-skills/pdf") {
				t.Errorf("Root = %q, want it to end with document-skills/pdf", res.Root)
			}
			if _, err := os.Stat(filepath.Join(res.Root, "SKILL.md")); err != nil {
				t.Errorf("expected SKILL.md at subpath root: %v", err)
			}
		})
	}
}

// TestAuthHeaders checks each profile sets the provider-correct auth header.
func TestAuthHeaders(t *testing.T) {
	adv := advertisement([][2]string{{"3333333333333333333333333333333333333333", "refs/tags/v1.0.0"}})
	tests := []struct {
		forge      forgeCase
		wantHeader string
		wantValue  string
	}{
		{forgeCases[0], "Authorization", "Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0"}, // x-access-token:secret
		{forgeCases[1], "Private-Token", "secret"},                             // GitLab
		{forgeCases[2], "Authorization", "token secret"},                       // Forgejo
		{forgeCases[3], "Authorization", "Basic eC10b2tlbi1hdXRoOnNlY3JldA=="}, // x-token-auth:secret
	}
	for _, tt := range tests {
		t.Run(tt.forge.name, func(t *testing.T) {
			var seen http.Header
			srv := newTestServer(t, tt.forge, adv, nil, &seen)
			defer srv.Close()
			cl := clientFor(srv, tt.forge, "secret")
			if _, _, _, err := cl.Resolve(context.Background(), spec.SourceSpec{Owner: "owner", Repo: "skills"}); err != nil {
				t.Fatal(err)
			}
			if got := seen.Get(tt.wantHeader); got != tt.wantValue {
				t.Errorf("%s = %q, want %q", tt.wantHeader, got, tt.wantValue)
			}
		})
	}
}
