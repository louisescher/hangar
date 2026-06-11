package npmreg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/spec"
)

// makeTarGz builds a gzipped tar whose entries are nested under a single
// "package/" root, as npm tarballs are.
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		full := "package/" + name
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

func sha512SRI(data []byte) string {
	h := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(h[:])
}

// testRegistry serves a packument and tarball for one package version. It
// records the Authorization header seen on the packument request.
type testRegistry struct {
	srv      *httptest.Server
	authSeen string
}

func newRegistry(t *testing.T, pkg, version string, tarball []byte, integrity, shasum string) *testRegistry {
	t.Helper()
	reg := &testRegistry{}
	mux := http.NewServeMux()
	pkgPath := pkg
	if strings.HasPrefix(pkg, "@") {
		pkgPath = strings.Replace(pkg, "/", "%2f", 1)
	}
	tarballPath := "/tarball/" + version + ".tgz"

	mux.HandleFunc("/"+pkgPath, func(w http.ResponseWriter, r *http.Request) {
		reg.authSeen = r.Header.Get("Authorization")
		pm := map[string]any{
			"dist-tags": map[string]string{"latest": version},
			"versions": map[string]any{
				version: map[string]any{
					"version": version,
					"dist": map[string]string{
						"tarball":   reg.srv.URL + tarballPath,
						"integrity": integrity,
						"shasum":    shasum,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pm)
	})
	mux.HandleFunc(tarballPath, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	})

	reg.srv = httptest.NewServer(mux)
	t.Cleanup(reg.srv.Close)
	return reg
}

func testClient(t *testing.T, reg *testRegistry, npmrc string) *Client {
	t.Helper()
	rc := config.ParseNPMRC(npmrc)
	return New(reg.srv.Client(), rc)
}

func TestFetchResolvesLatestAndExtracts(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{
		"SKILL.md":    "---\nname: demo\ndescription: a demo\n---\nbody\n",
		"README.md":   "# Demo\n",
		"docs/api.md": "# API\n",
	})
	reg := newRegistry(t, "demo-pkg", "1.2.0", tarball, sha512SRI(tarball), "")
	c := testClient(t, reg, "registry="+reg.srv.URL+"/\n")

	res, err := c.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindNPM, Pkg: "demo-pkg"})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Cleanup()

	if res.Ref != "1.2.0" || res.IsTag {
		t.Errorf("got ref=%q isTag=%v, want 1.2.0/false", res.Ref, res.IsTag)
	}
	for _, f := range []string{"SKILL.md", "README.md", filepath.Join("docs", "api.md")} {
		if _, err := os.Stat(filepath.Join(res.Root, f)); err != nil {
			t.Errorf("expected %s in extracted root: %v", f, err)
		}
	}
}

func TestResolvePinnedVersion(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{"SKILL.md": "x"})
	reg := newRegistry(t, "demo-pkg", "2.0.0", tarball, sha512SRI(tarball), "")
	c := testClient(t, reg, "registry="+reg.srv.URL+"/\n")

	// Exact pinned version present in the packument resolves.
	ref, _, _, err := c.Resolve(context.Background(), spec.SourceSpec{Kind: spec.KindNPM, Pkg: "demo-pkg", Ref: "2.0.0", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "2.0.0" {
		t.Errorf("ref = %q, want 2.0.0", ref)
	}

	// A missing version is an error (no range resolution).
	if _, _, _, err := c.Resolve(context.Background(), spec.SourceSpec{Kind: spec.KindNPM, Pkg: "demo-pkg", Ref: "9.9.9", Pinned: true}); err == nil {
		t.Error("expected error for missing version, got nil")
	}
}

func TestResolveDistTag(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{"SKILL.md": "x"})
	reg := newRegistry(t, "demo-pkg", "1.0.0", tarball, sha512SRI(tarball), "")
	c := testClient(t, reg, "registry="+reg.srv.URL+"/\n")

	// "latest" is a dist-tag pointing at 1.0.0.
	ref, _, _, err := c.Resolve(context.Background(), spec.SourceSpec{Kind: spec.KindNPM, Pkg: "demo-pkg", Ref: "latest", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "1.0.0" {
		t.Errorf("dist-tag latest resolved to %q, want 1.0.0", ref)
	}
}

func TestFetchSubpath(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{
		"skills/pdf/SKILL.md": "---\nname: pdf\n---\n",
		"README.md":           "# root\n",
	})
	reg := newRegistry(t, "demo-pkg", "1.0.0", tarball, sha512SRI(tarball), "")
	c := testClient(t, reg, "registry="+reg.srv.URL+"/\n")

	res, err := c.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindNPM, Pkg: "demo-pkg", Subpath: "skills/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Cleanup()
	if !strings.HasSuffix(filepath.ToSlash(res.Root), "skills/pdf") {
		t.Errorf("root %q should end with skills/pdf", res.Root)
	}
	if _, err := os.Stat(filepath.Join(res.Root, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md missing at subpath root: %v", err)
	}
}

func TestIntegrityMismatchFails(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{"SKILL.md": "x"})
	// Advertise the integrity of different bytes.
	wrong := sha512SRI([]byte("not the tarball"))
	reg := newRegistry(t, "demo-pkg", "1.0.0", tarball, wrong, "")
	c := testClient(t, reg, "registry="+reg.srv.URL+"/\n")

	if _, err := c.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindNPM, Pkg: "demo-pkg"}); err == nil {
		t.Fatal("expected integrity mismatch error, got nil")
	}
}

func TestScopedRegistryAuthHeader(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{"SKILL.md": "x"})
	reg := newRegistry(t, "@acme/widgets", "1.0.0", tarball, sha512SRI(tarball), "")

	host := strings.TrimPrefix(reg.srv.URL, "http:")
	npmrc := fmt.Sprintf("@acme:registry=%s/\n%s/:_authToken=topsecret\n", reg.srv.URL, host)
	c := testClient(t, reg, npmrc)

	if _, err := c.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindNPM, Pkg: "@acme/widgets"}); err != nil {
		t.Fatal(err)
	}
	if reg.authSeen != "Bearer topsecret" {
		t.Errorf("Authorization header = %q, want \"Bearer topsecret\"", reg.authSeen)
	}
}
