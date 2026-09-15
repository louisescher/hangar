package engine

import (
	"testing"

	"github.com/louisescher/hangar/internal/lockfile"
	"github.com/louisescher/hangar/internal/spec"
)

// TestForgeLockRoundTrip checks that a spec rendered into a lockfile source
// string reconstructs to the same source via specFromEntry, for every kind.
func TestForgeLockRoundTrip(t *testing.T) {
	e := &Engine{hostForges: map[string]spec.Forge{"git.company.com": spec.ForgeGitLab}}

	tests := []struct {
		name       string
		in         spec.SourceSpec
		wantSource string
	}{
		{
			name:       "github bare",
			in:         spec.SourceSpec{Kind: spec.KindGitHub, Owner: "anthropics", Repo: "skills", Subpath: "a/b"},
			wantSource: "anthropics/skills",
		},
		{
			name:       "npm",
			in:         spec.SourceSpec{Kind: spec.KindNPM, Pkg: "zod"},
			wantSource: "npm:zod",
		},
		{
			name:       "gitlab public",
			in:         spec.SourceSpec{Kind: spec.KindGit, Forge: spec.ForgeGitLab, Host: "https://gitlab.com", Owner: "group", Repo: "proj", Subpath: "sub"},
			wantSource: "https://gitlab.com/group/proj",
		},
		{
			name:       "self-hosted gitlab",
			in:         spec.SourceSpec{Kind: spec.KindGit, Forge: spec.ForgeGitLab, Host: "https://git.company.com", Owner: "team", Repo: "repo"},
			wantSource: "https://git.company.com/team/repo",
		},
		{
			name:       "tangled",
			in:         spec.SourceSpec{Kind: spec.KindGit, Forge: spec.ForgeTangled, Host: "https://tangled.org", Owner: "socialde.pt", Repo: "atproto.md", Subpath: "sub"},
			wantSource: "https://tangled.org/socialde.pt/atproto.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := lockSource(tt.in)
			if src != tt.wantSource {
				t.Fatalf("lockSource = %q, want %q", src, tt.wantSource)
			}
			entry := lockfile.Entry{Source: src, Subpath: tt.in.Subpath, Pinned: tt.in.Pinned}
			got, err := e.specFromEntry(entry)
			if err != nil {
				t.Fatalf("specFromEntry(%q): %v", src, err)
			}
			if got.Kind != tt.in.Kind || got.Forge != tt.in.Forge || got.Host != tt.in.Host ||
				got.Owner != tt.in.Owner || got.Repo != tt.in.Repo || got.Subpath != tt.in.Subpath {
				t.Errorf("round-trip mismatch\n got = %+v\nwant kind/forge/host/owner/repo/subpath = %v/%v/%v/%v/%v/%v",
					got, tt.in.Kind, tt.in.Forge, tt.in.Host, tt.in.Owner, tt.in.Repo, tt.in.Subpath)
			}
		})
	}
}

// TestSpecFromEntryUnregisteredHostIsHTTP ensures a lockfile source on a host
// that is not a known forge reconstructs as a bare HTTP skill source.
func TestSpecFromEntryUnregisteredHostIsHTTP(t *testing.T) {
	e := &Engine{hostForges: nil}
	got, err := e.specFromEntry(lockfile.Entry{Source: "https://atproto.md/skill.md"})
	if err != nil {
		t.Fatalf("specFromEntry: %v", err)
	}
	if got.Kind != spec.KindHTTP || got.URL != "https://atproto.md/skill.md" {
		t.Errorf("got kind=%v url=%q, want http/url", got.Kind, got.URL)
	}
}

// TestSpecFromEntryRegisteredHostStaysGit ensures a lockfile source on a
// registered forge host still reconstructs as git, not HTTP.
func TestSpecFromEntryRegisteredHostStaysGit(t *testing.T) {
	e := &Engine{hostForges: map[string]spec.Forge{"git.company.com": spec.ForgeGitLab}}
	got, err := e.specFromEntry(lockfile.Entry{Source: "https://git.company.com/team/repo"})
	if err != nil {
		t.Fatalf("specFromEntry: %v", err)
	}
	if got.Kind != spec.KindGit || got.Forge != spec.ForgeGitLab {
		t.Errorf("got kind=%v forge=%v, want git/gitlab", got.Kind, got.Forge)
	}
}
