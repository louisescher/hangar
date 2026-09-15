// Package spec parses Hangar source specifiers into a structured SourceSpec.
//
// Grammar (in precedence order):
//
//	local   := "file://"<path> | "."|".."|"~"  | ("./"|"../"|"~/"|"/")<path>
//	npm     := "npm:" ["@"<scope> "/"] <pkg> ["/" <subpath>] ["@" <version>] ["#" <file>]
//	ghURL   := ["https://"|"http://"|"ssh://"|"git@"] ["www."] "github.com" ("/"|":")
//	           <owner> "/" <repo>[".git"] ["/" ("tree"|"blob") "/" <ref> ["/" <subpath>]]
//	forgeURL := ["https://"|"http://"|"ssh://"|"git@"] <host> ("/"|":")
//	           <owner> "/" <repo>[".git"] [<provider marker> "/" <ref> ["/" <subpath>]]
//	github  := <owner> "/" <repo> ["/" <subpath>] ["@" <ref>] ["#" <skill>]
//	tangled := "tangled:" <owner> "/" <repo> ["/" <subpath>] ["@" <ref>] ["#" <skill>]
//
// Disambiguation is order-sensitive and deliberate:
//   - Local paths are checked first, because a relative path can otherwise look
//     like an "owner/repo" pair.
//   - A github.com web/clone URL (with or without scheme) is recognized before
//     the bare owner/repo form so a pasted browser link "just works". The ref in
//     a "/tree/<ref>/..." or "/blob/<ref>/..." URL is taken as the single segment
//     after tree/blob, so a branch name containing "/" can't be disambiguated
//     from a URL — use the owner/repo/sub@ref form for those. Only github.com is
//     recognized as the bare-form default.
//   - Non-github.com forge URLs (GitLab, Bitbucket, Forgejo/Gitea, …) are parsed
//     by parseForgeURL into KindGit, with the host carried on the spec. The
//     public hosts (gitlab.com, bitbucket.org, codeberg.org) are built in;
//     self-hosted hosts are resolved via the map passed to ParseWithForges. Each
//     provider's browser-URL markers supply the ref/subpath (GitLab "/-/tree/",
//     Forgejo/Gitea "/src/branch/", Bitbucket "/src/").
//   - For GitHub, the "#skill" suffix is peeled before the "@ref" suffix: the
//     grammar places #skill last, so given owner/repo@v2#bar we must remove
//     "#bar" first, then take "@v2" as the ref. The ref is taken from the LAST
//     "@" so refs may themselves contain "/" (e.g. release/1.x). Owner, repo and
//     subpath never contain "@", which keeps this unambiguous.
//   - A bare "owner/repo" is GitHub; Tangled requires the "tangled:" prefix or a
//     tangled.org/tangled.sh host, so the two never collide.
package spec

import (
	"fmt"
	"net/url"
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
	KindGit // any non-GitHub git forge (host carried in Host/Forge)
	KindHTTP
)

func (k Kind) String() string {
	switch k {
	case KindGitHub:
		return "github"
	case KindLocal:
		return "local"
	case KindNPM:
		return "npm"
	case KindGit:
		return "git"
	case KindHTTP:
		return "http"
	default:
		return "unknown"
	}
}

// IsGitTarball reports whether the kind is fetched as a git-host tarball (a
// repository resolved by ref/SHA), which covers both GitHub and the generic
// git forges. Engine code that special-cases "git repo with a ref" should gate
// on this rather than KindGitHub alone.
func (k Kind) IsGitTarball() bool { return k == KindGitHub || k == KindGit }

// Forge identifies the API/URL dialect of a git host. GitHub keeps its own Kind
// and dedicated fetcher; the rest are served by the generic gitforge fetcher.
type Forge string

const (
	ForgeGitHub    Forge = "github"
	ForgeGitLab    Forge = "gitlab"
	ForgeForgejo   Forge = "forgejo" // also Gitea and Codeberg
	ForgeBitbucket Forge = "bitbucket"
	ForgeTangled   Forge = "tangled" // tangled.org and its tangled.sh alias
	ForgeGeneric   Forge = "generic"
)

// ForgeFromString maps a config "type" value to a Forge. "gitea" normalizes to
// ForgeForgejo (same URL dialect).
func ForgeFromString(s string) (Forge, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "github":
		return ForgeGitHub, true
	case "gitlab":
		return ForgeGitLab, true
	case "forgejo", "gitea":
		return ForgeForgejo, true
	case "bitbucket":
		return ForgeBitbucket, true
	case "tangled":
		return ForgeTangled, true
	case "generic":
		return ForgeGeneric, true
	}
	return "", false
}

// SourceSpec is the parsed form of a source specifier.
type SourceSpec struct {
	Kind Kind

	// GitHub / git forges
	Owner string
	Repo  string
	Host  string // full origin for KindGit (e.g. "https://gitlab.com"); "" for GitHub
	Forge Forge  // forge dialect for KindGit

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

	URL string

	Raw string // the original, unparsed input
}

// Parse turns a source specifier string into a SourceSpec. Only github.com URLs
// and the public forge hosts are recognized; self-hosted hosts require
// ParseWithForges with a host→forge map.
func Parse(s string) (SourceSpec, error) {
	return ParseWithForges(s, nil)
}

// ParseWithForges is Parse with an additional map of self-hosted host →
// Forge (built by the engine from the user's config file). github.com and the
// built-in public hosts are always recognized; the map only extends them.
func ParseWithForges(s string, hosts map[string]Forge) (SourceSpec, error) {
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
	case strings.HasPrefix(s, "tangled:"):
		return parseTangledPrefix(strings.TrimPrefix(s, "tangled:"), raw, hosts)
	case isGitHubURL(s):
		return parseGitHubURL(s, raw)
	case isBareHTTPFile(s, hosts):
		return parseHTTP(s, raw)
	case isForgeURL(s):
		return parseForgeURL(s, raw, hosts)
	default:
		return parseGitHub(s, raw)
	}
}

// publicForgeHosts maps the well-known public forge hosts to their dialect.
// Self-hosted hosts are supplied via ParseWithForges' map.
var publicForgeHosts = map[string]Forge{
	"bitbucket.org": ForgeBitbucket,
	"codeberg.org":  ForgeForgejo,
	"gitlab.com":    ForgeGitLab,
	"tangled.org":   ForgeTangled,
	"tangled.sh":    ForgeTangled,
}

// ForgeForHost resolves a host to its forge dialect, consulting the built-in
// public hosts first and then the caller-supplied self-hosted map.
func ForgeForHost(host string, hosts map[string]Forge) (Forge, bool) {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	if f, ok := publicForgeHosts[host]; ok {
		return f, true
	}
	if f, ok := hosts[host]; ok {
		return f, true
	}
	return "", false
}

func isBareHTTPFile(s string, hosts map[string]Forge) bool {
	l := strings.ToLower(s)
	if !strings.HasPrefix(l, "http://") && !strings.HasPrefix(l, "https://") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if _, ok := ForgeForHost(u.Hostname(), hosts); ok {
		return false
	}
	return strings.Trim(u.Path, "/") != ""
}

// isForgeURL reports whether s is a non-github.com git host URL (https/http/ssh
// clone or browser link, or a scheme-less "host/owner/repo" where the first
// segment looks like a hostname). Checked after isGitHubURL, so github.com never
// reaches here. A bare "owner/repo" has no dot in its first segment and is left
// to the GitHub default.
func isForgeURL(s string) bool {
	l := strings.ToLower(s)
	for _, sc := range []string{"https://", "http://", "ssh://", "git@"} {
		if strings.HasPrefix(l, sc) {
			return true
		}
	}
	if i := strings.IndexByte(s, '/'); i > 0 {
		first := s[:i]
		if strings.Contains(first, ".") && !strings.Contains(first, "@") {
			return true
		}
	}
	return false
}

// stripSchemeHost splits a git URL into its host and the remaining path. It
// handles https/http/ssh schemes, the scp-like "git@host:path" form, and
// scheme-less "host/path".
func stripSchemeHost(s string) (host, rest string) {
	l := strings.ToLower(s)
	if strings.HasPrefix(l, "git@") {
		s = s[len("git@"):]
		if i := strings.IndexByte(s, ':'); i >= 0 { // scp form: host:path
			return s[:i], s[i+1:]
		}
		if i := strings.IndexByte(s, '/'); i >= 0 {
			return s[:i], s[i+1:]
		}
		return s, ""
	}
	for _, sc := range []string{"https://", "http://", "ssh://"} {
		if strings.HasPrefix(l, sc) {
			s = s[len(sc):]
			break
		}
	}
	s = strings.TrimPrefix(s, "git@") // ssh://git@host/...
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// parseTangledPrefix parses the "tangled:owner/repo[/subpath][@ref][#skill]"
// shorthand by routing it through the forge parser as a tangled.org host.
func parseTangledPrefix(body, raw string, hosts map[string]Forge) (SourceSpec, error) {
	if body == "" {
		return SourceSpec{}, fmt.Errorf("invalid forge URL %q: missing owner/repo", raw)
	}
	return parseForgeURL("tangled.org/"+body, raw, hosts)
}

// parseForgeURL turns a non-github.com forge URL into a KindGit SourceSpec.
// URLs with an explicit scheme are treated as browser/clone links (query and
// fragment dropped; the ref/subpath come from the provider's path markers).
// Scheme-less input is treated as the shorthand grammar, peeling "#skill" and
// "@ref" like the bare GitHub form.
func parseForgeURL(s, raw string, hosts map[string]Forge) (SourceSpec, error) {
	hadScheme := false
	l := strings.ToLower(s)
	for _, sc := range []string{"https://", "http://", "ssh://", "git@"} {
		if strings.HasPrefix(l, sc) {
			hadScheme = true
			break
		}
	}

	host, rest := stripSchemeHost(s)
	host = strings.ToLower(host)
	if host == "" {
		return SourceSpec{}, fmt.Errorf("invalid forge URL %q: missing host", raw)
	}
	forge, ok := ForgeForHost(host, hosts)
	if !ok {
		return SourceSpec{}, fmt.Errorf("unknown git host %q: register it in ~/.config/hangar/config.toml under [forges.hosts.%q] with type = \"gitlab\" | \"forgejo\" | \"gitea\" | \"bitbucket\" | \"tangled\" | \"generic\"", host, host)
	}

	sp := SourceSpec{Kind: KindGit, Forge: forge, Host: "https://" + host, Raw: raw}

	var ref, skill string
	var pinned bool
	if hadScheme {
		if i := strings.IndexAny(rest, "?#"); i >= 0 {
			rest = rest[:i]
		}
	} else {
		rest, ref, skill, pinned = peelRefAndSkill(rest)
	}

	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return SourceSpec{}, fmt.Errorf("invalid forge URL %q: expected host/owner/repo", raw)
	}
	sp.Owner = parts[0]
	sp.Repo = strings.TrimSuffix(parts[1], ".git")
	if sp.Repo == "" {
		return SourceSpec{}, fmt.Errorf("invalid forge URL %q: expected host/owner/repo", raw)
	}

	if len(parts) > 2 {
		mref, sub, err := parseForgeMarker(forge, parts[2:], raw)
		if err != nil {
			return SourceSpec{}, err
		}
		if mref != "" {
			ref, pinned = mref, true
		}
		if sub != "" {
			cleaned, err := cleanSubpath(sub, raw)
			if err != nil {
				return SourceSpec{}, err
			}
			sp.Subpath = cleaned
		}
	}

	sp.Ref = ref
	sp.Pinned = pinned
	if skill != "" {
		if strings.Contains(skill, "/") {
			return SourceSpec{}, fmt.Errorf("invalid #skill %q: must not contain '/'", skill)
		}
		sp.Skill = skill
	}
	return sp, nil
}

// parseForgeMarker interprets a forge's browser-URL path markers (the segments
// after owner/repo) into a ref and a subpath. Each provider uses a different
// marker layout. When no marker matches, the segments are treated as a subpath.
func parseForgeMarker(forge Forge, segs []string, raw string) (ref, sub string, err error) {
	switch forge {
	case ForgeGitLab:
		// /-/tree/<ref>/<sub>, /-/blob/<ref>/<file>
		if len(segs) >= 1 && segs[0] == "-" {
			if len(segs) < 3 || segs[2] == "" {
				return "", "", fmt.Errorf("invalid GitLab URL %q: %s needs a ref", raw, strings.Join(segs, "/"))
			}
			return segs[2], blobDir(segs[1], strings.Join(segs[3:], "/")), nil
		}
	case ForgeForgejo, ForgeGeneric:
		// /src/branch|tag|commit/<ref>/<sub>, /raw/branch/<ref>/<file>
		if len(segs) >= 2 && (segs[0] == "src" || segs[0] == "raw") {
			switch segs[1] {
			case "branch", "tag", "commit":
				if len(segs) < 3 || segs[2] == "" {
					return "", "", fmt.Errorf("invalid Forgejo URL %q: %s/%s needs a ref", raw, segs[0], segs[1])
				}
				kind := "tree"
				if segs[0] == "raw" {
					kind = "blob"
				}
				return segs[2], blobDir(kind, strings.Join(segs[3:], "/")), nil
			}
		}
	case ForgeBitbucket:
		// /src/<ref>/<sub>
		if len(segs) >= 1 && segs[0] == "src" {
			if len(segs) < 2 || segs[1] == "" {
				return "", "", fmt.Errorf("invalid Bitbucket URL %q: src needs a ref", raw)
			}
			return segs[1], strings.Join(segs[2:], "/"), nil
		}
	case ForgeTangled:
		// /tree/<ref>/<sub>, /blob/<ref>/<file>
		if len(segs) >= 1 && (segs[0] == "tree" || segs[0] == "blob") {
			if len(segs) < 2 || segs[1] == "" {
				return "", "", fmt.Errorf("invalid Tangled URL %q: %s needs a ref", raw, segs[0])
			}
			return segs[1], blobDir(segs[0], strings.Join(segs[2:], "/")), nil
		}
	}
	return "", strings.Join(segs, "/"), nil
}

// blobDir roots a "blob"/"raw" file link at its containing directory so the
// surrounding skill (its SKILL.md) is discovered; "tree" links keep the path.
func blobDir(kind, sub string) string {
	if kind != "blob" || sub == "" {
		return sub
	}
	d := path.Dir(sub)
	if d == "." || d == "/" {
		return ""
	}
	return d
}

// peelRefAndSkill strips a trailing "#skill" then "@ref" from a bare spec body,
// matching the GitHub shorthand grammar. The ref is taken from the LAST '@' so
// refs may themselves contain '/'.
func peelRefAndSkill(body string) (rest, ref, skill string, pinned bool) {
	rest = body
	if i := strings.Index(rest, "#"); i >= 0 {
		skill = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		ref = rest[i+1:]
		pinned = true
		rest = rest[:i]
	}
	return rest, ref, skill, pinned
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

func parseHTTP(s, raw string) (SourceSpec, error) {
	u, err := url.Parse(s)
	if err != nil {
		return SourceSpec{}, fmt.Errorf("invalid HTTP URL %q: %w", raw, err)
	}
	u.Fragment = ""
	return SourceSpec{Kind: KindHTTP, URL: u.String(), Raw: raw}, nil
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

	// Peel "#skill" (grammar-last) then "@ref" (last '@', so refs may contain '/').
	body, sp.Ref, sp.Skill, sp.Pinned = peelRefAndSkill(body)

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
