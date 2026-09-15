package httpfile

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/louisescher/hangar/internal/fetch"
	"github.com/louisescher/hangar/internal/spec"
)

type fakeDoer struct {
	status int
	body   string
	err    error
}

func (d fakeDoer) Do(*http.Request) (*http.Response, error) {
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{
		StatusCode: d.status,
		Body:       io.NopCloser(strings.NewReader(d.body)),
	}, nil
}

const goodSkill = "---\nname: atproto\ndescription: Work with atproto.\n---\n\n# Atproto\n"

func TestFetchWritesSkill(t *testing.T) {
	f := New(fakeDoer{status: 200, body: goodSkill})
	res, err := f.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindHTTP, URL: "https://atproto.md/skill.md"})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Cleanup()
	if res.SHA == "" {
		t.Error("expected a content hash")
	}
	if filepath.Base(res.Root) != "skill" {
		t.Errorf("root base = %q, want skill", filepath.Base(res.Root))
	}
	b, err := os.ReadFile(filepath.Join(res.Root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != goodSkill {
		t.Errorf("SKILL.md = %q", string(b))
	}
}

func TestFetchNotFound(t *testing.T) {
	f := New(fakeDoer{status: 404})
	_, err := f.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindHTTP, URL: "https://atproto.md/skill.md"})
	if !errors.Is(err, fetch.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFetchRejectsInvalid(t *testing.T) {
	for _, body := range []string{
		"# no frontmatter\n",
		"---\nname: atproto\n---\n\nno description\n",
		"---\nname: [broken\n---\n",
	} {
		f := New(fakeDoer{status: 200, body: body})
		_, err := f.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindHTTP, URL: "https://atproto.md/skill.md"})
		if !errors.Is(err, fetch.ErrNotFound) {
			t.Errorf("body %q: err = %v, want ErrNotFound", body, err)
		}
	}
}

func TestResolveHashMatchesFetch(t *testing.T) {
	f := New(fakeDoer{status: 200, body: goodSkill})
	_, sha, _, err := f.Resolve(context.Background(), spec.SourceSpec{Kind: spec.KindHTTP, URL: "https://atproto.md/skill.md"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Fetch(context.Background(), spec.SourceSpec{Kind: spec.KindHTTP, URL: "https://atproto.md/skill.md"})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Cleanup()
	if sha != res.SHA {
		t.Errorf("resolve sha %q != fetch sha %q", sha, res.SHA)
	}
}

func TestSlugFor(t *testing.T) {
	tests := map[string]string{
		"https://atproto.md/skill.md":      "skill",
		"https://example.com/my-skill":     "my-skill",
		"https://example.com/a/b/thing.md": "thing",
		"https://example.com/":             "skill",
	}
	for in, want := range tests {
		if got := slugFor(in); got != want {
			t.Errorf("slugFor(%q) = %q, want %q", in, got, want)
		}
	}
}
