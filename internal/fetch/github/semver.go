package github

import (
	"regexp"
	"strconv"
	"strings"
)

// semver is a parsed version. Two-part tags (v0.5) are treated as v0.5.0.
type semver struct {
	major, minor, patch int
	pre                 string // prerelease identifier, "" for a release
}

// less reports whether a sorts before b (lower precedence).
func (a semver) less(b semver) bool {
	switch {
	case a.major != b.major:
		return a.major < b.major
	case a.minor != b.minor:
		return a.minor < b.minor
	case a.patch != b.patch:
		return a.patch < b.patch
	}
	// A release outranks a prerelease of the same x.y.z.
	switch {
	case a.pre == "" && b.pre == "":
		return false
	case a.pre == "":
		return false
	case b.pre == "":
		return true
	default:
		return a.pre < b.pre
	}
}

var verRe = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.(\d+))?(?:-(.+))?$`)

// parseTag extracts a version from a tag name, honoring monorepo-style prefixes
// (`<repo>@v1.2.3` and `<repo>-v1.2.3`). prefixed reports whether a repo prefix
// was present; ok reports whether the remainder parsed as a version.
func parseTag(tag, repo string) (v semver, prefixed, ok bool) {
	rest := tag
	switch {
	case strings.HasPrefix(tag, repo+"@"):
		rest, prefixed = tag[len(repo)+1:], true
	case strings.HasPrefix(tag, repo+"-"):
		rest, prefixed = tag[len(repo)+1:], true
	}

	m := verRe.FindStringSubmatch(rest)
	if m == nil {
		return semver{}, prefixed, false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch := 0
	if m[3] != "" {
		patch, _ = strconv.Atoi(m[3])
	}
	return semver{major: major, minor: minor, patch: patch, pre: m[4]}, prefixed, true
}

// selectLatestTag picks the highest non-prerelease semver tag. When any
// repo-prefixed tags exist, only those are considered (monorepo precedence).
// tags maps tag name (without refs/tags/) to its commit SHA.
func selectLatestTag(tags map[string]string, repo string) (tag, sha string, ok bool) {
	type cand struct {
		ver semver
		tag string
	}
	var all, prefixed []cand
	for name := range tags {
		v, isPrefixed, parsed := parseTag(name, repo)
		if !parsed || v.pre != "" { // drop prereleases
			continue
		}
		c := cand{ver: v, tag: name}
		all = append(all, c)
		if isPrefixed {
			prefixed = append(prefixed, c)
		}
	}

	pool := all
	if len(prefixed) > 0 {
		pool = prefixed
	}
	if len(pool) == 0 {
		return "", "", false
	}

	best := pool[0]
	for _, c := range pool[1:] {
		if best.ver.less(c.ver) {
			best = c
		}
	}
	return best.tag, tags[best.tag], true
}
