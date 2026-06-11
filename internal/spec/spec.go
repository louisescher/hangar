// Package spec parses Hangar source specifiers into a structured SourceSpec.
//
// Grammar (in precedence order):
//
//	local   := "file://"<path> | "."|".."|"~"  | ("./"|"../"|"~/"|"/")<path>
//	npm     := "npm:" ["@"<scope> "/"] <pkg> ["/" <subpath>] ["@" <version>] ["#" <file>]
//	ghURL   := ["https://"|"http://"|"ssh://"|"git@"] ["www."] "github.com" ("/"|":")
//	           <owner> "/" <repo>[".git"] ["/" ("tree"|"blob") "/" <ref> ["/" <subpath>]]
//	github  := <owner> "/" <repo> ["/" <subpath>] ["@" <ref>] ["#" <skill>]
//
// Disambiguation is order-sensitive and deliberate:
//   - Local paths are checked first, because a relative path can otherwise look
//     like an "owner/repo" pair.
//   - A github.com web/clone URL (with or without scheme) is recognized before
//     the bare owner/repo form so a pasted browser link "just works". The ref in
//     a "/tree/<ref>/..." or "/blob/<ref>/..." URL is taken as the single segment
//     after tree/blob, so a branch name containing "/" can't be disambiguated
//     from a URL — use the owner/repo/sub@ref form for those. Only github.com is
//     recognized (not GitHub Enterprise hosts).
//   - For GitHub, the "#skill" suffix is peeled before the "@ref" suffix: the
//     grammar places #skill last, so given owner/repo@v2#bar we must remove
//     "#bar" first, then take "@v2" as the ref. The ref is taken from the LAST
//     "@" so refs may themselves contain "/" (e.g. release/1.x). Owner, repo and
//     subpath never contain "@", which keeps this unambiguous.
package spec

import (
	"fmt"
	"os"
	"path"
	"strings"
)

// Kind identifies the source family of a SourceSpec.
type Kind int

const (
	KindGitHub Kind = iota
	KindLocal
	KindNPM
)

func (k Kind) String() string {
	switch k {
	case KindGitHub:
		return "github"
	case KindLocal:
		return "local"
	case KindNPM:
		return "npm"
	default:
		return "unknown"
	}
}

// SourceSpec is the parsed form of a source specifier.
type SourceSpec struct {
	Kind Kind

	// GitHub
	Owner string
	Repo  string

	// npm
	Pkg  string // package name, including @scope for scoped packages
	File string // "#file" — a specific reference doc within the package

	// Shared
	Subpath string // roots the skill crawl to a subdirectory (GitHub repo or npm pkg)
	Ref     string // branch / tag / commit (GitHub) or version (npm); "" = resolve latest
	Pinned  bool   // true when @ref (or @version) was given explicitly
	Skill   string // "#skill" — install only the skill with this name

	// local
	Path string // filesystem path, ~-expanded and cleaned

	Raw string // the original, unparsed input
}

// Parse turns a source specifier string into a SourceSpec.
func Parse(s string) (SourceSpec, error) {
	raw := s
	s = strings.TrimSpace(s)
	if s == "" {
		return SourceSpec{}, fmt.Errorf("empty source spec")
	}

	switch {
	case isLocal(s):
		return parseLocal(s, raw)
	case strings.HasPrefix(s, "npm:"):
		return parseNPM(strings.TrimPrefix(s, "npm:"), raw)
	case isGitHubURL(s):
		return parseGitHubURL(s, raw)
	default:
		return parseGitHub(s, raw)
	}
}

// gitHubURLPrefixes are the recognized leading forms of a github.com URL.
var gitHubURLPrefixes = []string{
	"https://github.com/", "http://github.com/",
	"https://www.github.com/", "http://www.github.com/",
	"ssh://git@github.com/", "git@github.com:",
	"github.com/", "www.github.com/",
}

// isGitHubURL reports whether s is a github.com web/clone URL (https, ssh, or
// scheme-less) rather than the bare owner/repo spec form.
func isGitHubURL(s string) bool {
	l := strings.ToLower(s)
	for _, p := range gitHubURLPrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// parseGitHubURL turns a github.com URL into a GitHub SourceSpec. It accepts
// browser links (".../tree/<ref>/<subpath>", ".../blob/<ref>/<file>") as well as
// https/ssh clone URLs. A "/tree/<ref>" segment supplies the ref (pinned) and any
// trailing path becomes the subpath; for a "/blob/<ref>/<file>" link the skill's
// containing directory is used as the subpath.
func parseGitHubURL(s, raw string) (SourceSpec, error) {
	rest := s

	// Strip scheme + host, leaving "owner/repo[/tree|blob/<ref>/<path...>]".
	if strings.HasPrefix(strings.ToLower(rest), "git@github.com:") {
		rest = rest[len("git@github.com:"):]
	} else {
		for _, sc := range []string{"https://", "http://", "ssh://"} {
			if strings.HasPrefix(strings.ToLower(rest), sc) {
				rest = rest[len(sc):]
				break
			}
		}
		rest = strings.TrimPrefix(rest, "git@")
		lower := strings.ToLower(rest)
		switch {
		case strings.HasPrefix(lower, "www.github.com/"):
			rest = rest[len("www.github.com/"):]
		case strings.HasPrefix(lower, "github.com/"):
			rest = rest[len("github.com/"):]
		}
	}

	// Drop any query string or fragment a browser URL may carry (e.g. "?tab=…",
	// "#L10"); these don't map to a skill filter.
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.Trim(rest, "/")

	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return SourceSpec{}, fmt.Errorf("invalid GitHub URL %q: expected github.com/owner/repo", raw)
	}

	sp := SourceSpec{Kind: KindGitHub, Raw: raw}
	sp.Owner = parts[0]
	sp.Repo = strings.TrimSuffix(parts[1], ".git")
	if sp.Repo == "" {
		return SourceSpec{}, fmt.Errorf("invalid GitHub URL %q: expected github.com/owner/repo", raw)
	}

	if len(parts) <= 2 {
		return sp, nil
	}

	switch parts[2] {
	case "tree", "blob":
		if len(parts) < 4 || parts[3] == "" {
			return SourceSpec{}, fmt.Errorf("invalid GitHub URL %q: %s needs a ref", raw, parts[2])
		}
		sp.Ref = parts[3]
		sp.Pinned = true
		sub := strings.Join(parts[4:], "/")
		if parts[2] == "blob" {
			// A blob points at a file; root the crawl at its directory so the
			// surrounding skill (its SKILL.md) is discovered.
			sub = path.Dir(sub)
			if sub == "." || sub == "/" {
				sub = ""
			}
		}
		if sub != "" {
			cleaned, err := cleanSubpath(sub, raw)
			if err != nil {
				return SourceSpec{}, err
			}
			sp.Subpath = cleaned
		}
	default:
		// Trailing segments without tree/blob: treat them as a subpath.
		cleaned, err := cleanSubpath(strings.Join(parts[2:], "/"), raw)
		if err != nil {
			return SourceSpec{}, err
		}
		sp.Subpath = cleaned
	}

	return sp, nil
}

// isLocal reports whether s should be treated as a filesystem path rather than
// an owner/repo pair.
func isLocal(s string) bool {
	switch {
	case strings.HasPrefix(s, "file://"):
		return true
	case s == "." || s == "..", s == "~":
		return true
	case strings.HasPrefix(s, "./"), strings.HasPrefix(s, "../"):
		return true
	case strings.HasPrefix(s, "/"):
		return true
	case s == "~" || strings.HasPrefix(s, "~/"):
		return true
	default:
		return false
	}
}

func parseLocal(s, raw string) (SourceSpec, error) {
	p := strings.TrimPrefix(s, "file://")
	expanded, err := expandHome(p)
	if err != nil {
		return SourceSpec{}, err
	}
	// path.Clean keeps relative paths relative; the local fetcher resolves them
	// against the working directory. ".." is legitimate for a local source.
	return SourceSpec{
		Kind: KindLocal,
		Path: path.Clean(expanded),
		Raw:  raw,
	}, nil
}

func parseNPM(body, raw string) (SourceSpec, error) {
	if body == "" {
		return SourceSpec{}, fmt.Errorf("npm spec missing package name: %q", raw)
	}

	sp := SourceSpec{Kind: KindNPM, Raw: raw}

	// Peel "#file" on the last '#'.
	if i := strings.LastIndex(body, "#"); i >= 0 {
		sp.File = body[i+1:]
		body = body[:i]
	}

	// Peel an optional "@version". A scoped package begins with '@' at index 0,
	// so only treat an '@' beyond index 0 as a version separator.
	if i := strings.LastIndex(body, "@"); i > 0 {
		sp.Ref = body[i+1:]
		sp.Pinned = true
		body = body[:i]
	}

	// Split the remaining body into package name and optional subpath.
	if strings.HasPrefix(body, "@") {
		// Scoped: @scope/pkg[/subpath...]
		parts := strings.SplitN(body, "/", 3)
		if len(parts) < 2 || parts[0] == "@" || parts[1] == "" {
			return SourceSpec{}, fmt.Errorf("invalid scoped npm package: %q", raw)
		}
		sp.Pkg = parts[0] + "/" + parts[1]
		if len(parts) == 3 {
			sub, err := cleanSubpath(parts[2], raw)
			if err != nil {
				return SourceSpec{}, err
			}
			sp.Subpath = sub
		}
	} else {
		// Unscoped: pkg[/subpath...]
		parts := strings.SplitN(body, "/", 2)
		sp.Pkg = parts[0]
		if sp.Pkg == "" {
			return SourceSpec{}, fmt.Errorf("npm spec missing package name: %q", raw)
		}
		if len(parts) == 2 {
			sub, err := cleanSubpath(parts[1], raw)
			if err != nil {
				return SourceSpec{}, err
			}
			sp.Subpath = sub
		}
	}

	return sp, nil
}

func parseGitHub(body, raw string) (SourceSpec, error) {
	sp := SourceSpec{Kind: KindGitHub, Raw: raw}

	// Peel "#skill" first (the grammar puts it last, after any @ref).
	if i := strings.Index(body, "#"); i >= 0 {
		sp.Skill = body[i+1:]
		body = body[:i]
	}

	// Peel "@ref" on the LAST '@' so refs may contain '/'.
	if i := strings.LastIndex(body, "@"); i >= 0 {
		sp.Ref = body[i+1:]
		sp.Pinned = true
		body = body[:i]
	}

	parts := strings.Split(body, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return SourceSpec{}, fmt.Errorf("invalid GitHub spec %q: expected owner/repo[/subpath]", raw)
	}
	sp.Owner = parts[0]
	sp.Repo = parts[1]
	if len(parts) > 2 {
		sub, err := cleanSubpath(strings.Join(parts[2:], "/"), raw)
		if err != nil {
			return SourceSpec{}, err
		}
		sp.Subpath = sub
	}

	if sp.Skill != "" && strings.Contains(sp.Skill, "/") {
		return SourceSpec{}, fmt.Errorf("invalid #skill %q: must not contain '/'", sp.Skill)
	}

	return sp, nil
}

// cleanSubpath validates and normalizes a repo/package-internal subpath. It
// rejects absolute paths and any ".." traversal so a spec can never reach
// outside the fetched archive.
func cleanSubpath(p, raw string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("subpath %q in %q must be relative", p, raw)
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("subpath %q in %q escapes the repository root", p, raw)
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", fmt.Errorf("subpath %q in %q escapes the repository root", p, raw)
		}
	}
	return cleaned, nil
}

// expandHome replaces a leading "~" or "~/" with the user's home directory.
func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand ~: %w", err)
		}
		if p == "~" {
			return home, nil
		}
		return path.Join(home, p[2:]), nil
	}
	return p, nil
}
