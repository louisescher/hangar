// Package spec parses Hangar source specifiers into a structured SourceSpec.
//
// Grammar (in precedence order):
//
//	local   := "file://"<path> | "."|".."|"~"  | ("./"|"../"|"~/"|"/")<path>
//	npm     := "npm:" ["@"<scope> "/"] <pkg> ["/" <subpath>] ["@" <version>] ["#" <file>]
//	github  := <owner> "/" <repo> ["/" <subpath>] ["@" <ref>] ["#" <skill>]
//
// Disambiguation is order-sensitive and deliberate:
//   - Local paths are checked first, because a relative path can otherwise look
//     like an "owner/repo" pair.
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
	default:
		return parseGitHub(s, raw)
	}
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
