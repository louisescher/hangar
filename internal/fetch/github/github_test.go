package github

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

func newTestServer(t *testing.T, adv string, tarball []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/owner/skills.git/info/refs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte(adv))
	})
	// codeload: /owner/skills/tar.gz/<ref>
	mux.HandleFunc("/owner/skills/tar.gz/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	})
	return httptest.NewServer(mux)
}

func testClient(srv *httptest.Server) *Client {
	c := New(srv.Client(), "")
	c.BaseURL = srv.URL
	c.CodeloadURL = srv.URL
	return c
}

func TestResolveLatestTag(t *testing.T) {
	adv := advertisement([][2]string{
		{"1111111111111111111111111111111111111111", "refs/heads/main"},
		{"2222222222222222222222222222222222222222", "refs/tags/v1.0.0"},
		{"3333333333333333333333333333333333333333", "refs/tags/v1.2.0"},
	})
	srv := newTestServer(t, adv, nil)
	defer srv.Close()
	c := testClient(srv)

	ref, sha, isTag, err := c.Resolve(context.Background(), spec.SourceSpec{Owner: "owner", Repo: "skills"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "v1.2.0" || sha != "3333333333333333333333333333333333333333" || !isTag {
		t.Errorf("got ref=%q sha=%q isTag=%v, want v1.2.0/333.../true", ref, sha, isTag)
	}
}

func TestResolveExplicitBranch(t *testing.T) {
	adv := advertisement([][2]string{
		{"1111111111111111111111111111111111111111", "refs/heads/main"},
		{"3333333333333333333333333333333333333333", "refs/tags/v1.2.0"},
	})
	srv := newTestServer(t, adv, nil)
	defer srv.Close()
	c := testClient(srv)

	ref, sha, isTag, err := c.Resolve(context.Background(), spec.SourceSpec{Owner: "owner", Repo: "skills", Ref: "main", Pinned: true})
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
	srv := newTestServer(t, adv, tarball)
	defer srv.Close()
	c := testClient(srv)

	res, err := c.Fetch(context.Background(), spec.SourceSpec{Owner: "owner", Repo: "skills", Subpath: "document-skills/pdf"})
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
}
