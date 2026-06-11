package spec

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want SourceSpec
	}{
		{
			name: "github bare",
			in:   "anthropics/skills",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills"},
		},
		{
			name: "github with tag",
			in:   "anthropics/skills@v1.2.0",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills", Ref: "v1.2.0", Pinned: true},
		},
		{
			name: "github with skill filter",
			in:   "anthropics/skills#pdf",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills", Skill: "pdf"},
		},
		{
			name: "github subpath",
			in:   "anthropics/skills/document-skills/pdf",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills", Subpath: "document-skills/pdf"},
		},
		{
			name: "github subpath + ref + skill (ordering)",
			in:   "owner/repo/skills/foo@v2#bar",
			want: SourceSpec{Kind: KindGitHub, Owner: "owner", Repo: "repo", Subpath: "skills/foo", Ref: "v2", Pinned: true, Skill: "bar"},
		},
		{
			name: "github ref containing slash",
			in:   "owner/repo@release/1.x",
			want: SourceSpec{Kind: KindGitHub, Owner: "owner", Repo: "repo", Ref: "release/1.x", Pinned: true},
		},
		{
			name: "github subpath cleaned",
			in:   "owner/repo/a//b/",
			want: SourceSpec{Kind: KindGitHub, Owner: "owner", Repo: "repo", Subpath: "a/b"},
		},
		{
			name: "github url with tree/branch/subpath",
			in:   "https://github.com/seibert-external/seibert-skills/tree/main/skills/development",
			want: SourceSpec{Kind: KindGitHub, Owner: "seibert-external", Repo: "seibert-skills", Ref: "main", Pinned: true, Subpath: "skills/development"},
		},
		{
			name: "github url bare",
			in:   "https://github.com/anthropics/skills",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills"},
		},
		{
			name: "github url trailing slash",
			in:   "https://github.com/anthropics/skills/",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills"},
		},
		{
			name: "github url tree branch only",
			in:   "https://github.com/anthropics/skills/tree/v1.2.0",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills", Ref: "v1.2.0", Pinned: true},
		},
		{
			name: "github url blob to skill file roots at its dir",
			in:   "https://github.com/anthropics/skills/blob/main/document-skills/pdf/SKILL.md",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills", Ref: "main", Pinned: true, Subpath: "document-skills/pdf"},
		},
		{
			name: "github url with query and fragment dropped",
			in:   "https://github.com/anthropics/skills/tree/main/document-skills?tab=readme#top",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills", Ref: "main", Pinned: true, Subpath: "document-skills"},
		},
		{
			name: "github clone url with .git suffix",
			in:   "https://github.com/anthropics/skills.git",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills"},
		},
		{
			name: "github ssh clone url",
			in:   "git@github.com:anthropics/skills.git",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills"},
		},
		{
			name: "github scheme-less url",
			in:   "github.com/anthropics/skills/tree/main/web",
			want: SourceSpec{Kind: KindGitHub, Owner: "anthropics", Repo: "skills", Ref: "main", Pinned: true, Subpath: "web"},
		},
		{
			name: "npm unscoped",
			in:   "npm:zod",
			want: SourceSpec{Kind: KindNPM, Pkg: "zod"},
		},
		{
			name: "npm scoped",
			in:   "npm:@tanstack/react-query",
			want: SourceSpec{Kind: KindNPM, Pkg: "@tanstack/react-query"},
		},
		{
			name: "npm unscoped subpath",
			in:   "npm:zod/lib",
			want: SourceSpec{Kind: KindNPM, Pkg: "zod", Subpath: "lib"},
		},
		{
			name: "npm scoped subpath",
			in:   "npm:@tanstack/react-query/build",
			want: SourceSpec{Kind: KindNPM, Pkg: "@tanstack/react-query", Subpath: "build"},
		},
		{
			name: "npm file ref",
			in:   "npm:zod#README.md",
			want: SourceSpec{Kind: KindNPM, Pkg: "zod", File: "README.md"},
		},
		{
			name: "npm scoped file ref with path",
			in:   "npm:@tanstack/react-query#docs/a.md",
			want: SourceSpec{Kind: KindNPM, Pkg: "@tanstack/react-query", File: "docs/a.md"},
		},
		{
			name: "npm unscoped version",
			in:   "npm:zod@3.22.4",
			want: SourceSpec{Kind: KindNPM, Pkg: "zod", Ref: "3.22.4", Pinned: true},
		},
		{
			name: "npm scoped version (@ at index 0 is scope, not version)",
			in:   "npm:@tanstack/react-query@5.0.0",
			want: SourceSpec{Kind: KindNPM, Pkg: "@tanstack/react-query", Ref: "5.0.0", Pinned: true},
		},
		{
			name: "local dot",
			in:   ".",
			want: SourceSpec{Kind: KindLocal, Path: "."},
		},
		{
			name: "local relative",
			in:   "./my-skills/foo",
			want: SourceSpec{Kind: KindLocal, Path: "my-skills/foo"},
		},
		{
			name: "local parent traversal allowed for local sources",
			in:   "../shared/skill",
			want: SourceSpec{Kind: KindLocal, Path: "../shared/skill"},
		},
		{
			name: "local absolute",
			in:   "/opt/skills/x",
			want: SourceSpec{Kind: KindLocal, Path: "/opt/skills/x"},
		},
		{
			name: "local file url",
			in:   "file:///opt/skills/x",
			want: SourceSpec{Kind: KindLocal, Path: "/opt/skills/x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.in, err)
			}
			tt.want.Raw = tt.in
			if got != tt.want {
				t.Errorf("Parse(%q)\n got = %+v\nwant = %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseHomeExpansion(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	got, err := Parse("~/skills/local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Kind != KindLocal || got.Path != "/home/tester/skills/local" {
		t.Errorf("got %+v, want local path /home/tester/skills/local", got)
	}

	got, err = Parse("~")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Path != "/home/tester" {
		t.Errorf("got %q, want /home/tester", got.Path)
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",                                   // empty
		"   ",                                // blank
		"justowner",                          // single segment, not local/npm
		"npm:",                               // npm missing package
		"npm:@scope",                         // scoped npm missing package part
		"owner/repo/../secret",               // subpath traversal
		"owner/repo/a/../../etc",             // subpath traversal after clean
		"npm:zod/../../etc",                  // npm subpath traversal
		"owner/repo#a/b",                     // skill name with slash
		"https://github.com/owner",           // URL missing repo
		"https://github.com/owner/repo/tree", // tree without a ref
		"https://github.com/owner/repo/tree/main/../x", // URL subpath traversal
	}
	for _, in := range bad {
		t.Run(in, func(t *testing.T) {
			if got, err := Parse(in); err == nil {
				t.Errorf("Parse(%q) expected error, got %+v", in, got)
			}
		})
	}
}
