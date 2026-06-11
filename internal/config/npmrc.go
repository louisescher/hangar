package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultRegistry is the public npm registry used when no .npmrc overrides it.
const DefaultRegistry = "https://registry.npmjs.org/"

// NPMRC holds npm registry configuration resolved from .npmrc files: the
// default registry, per-scope registries, and per-registry auth tokens. It is
// the subset of npm config Hangar needs to fetch packuments and tarballs from
// public, scoped, and private registries.
type NPMRC struct {
	defaultRegistry string            // e.g. https://registry.npmjs.org/
	scopeRegistry   map[string]string // "@scope" -> registry URL
	authTokens      map[string]string // "//host/path/" (scheme-stripped) -> token
}

func defaultNPMRC() *NPMRC {
	return &NPMRC{
		defaultRegistry: DefaultRegistry,
		scopeRegistry:   map[string]string{},
		authTokens:      map[string]string{},
	}
}

// LoadNPMRC reads and merges npm configuration. Precedence, low to high:
// the built-in default, then ~/.npmrc, then ./.npmrc in the working directory,
// so a project-local file overrides the user's. Missing files are ignored.
func LoadNPMRC() *NPMRC {
	rc := defaultNPMRC()
	for _, f := range npmrcFiles() {
		if data, err := os.ReadFile(f); err == nil {
			rc.parse(string(data))
		}
	}
	return rc
}

// ParseNPMRC builds an NPMRC from .npmrc file contents, starting from the
// built-in defaults. It performs no file I/O.
func ParseNPMRC(content string) *NPMRC {
	rc := defaultNPMRC()
	rc.parse(content)
	return rc
}

func npmrcFiles() []string {
	var files []string
	if home, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(home, ".npmrc"))
	}
	if wd, err := os.Getwd(); err == nil {
		files = append(files, filepath.Join(wd, ".npmrc"))
	}
	return files
}

var envRefRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// parse merges the key/value lines of one .npmrc file into rc; later calls
// override earlier values for the same key.
func (n *NPMRC) parse(content string) {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = expandEnv(unquote(strings.TrimSpace(value)))

		switch {
		case key == "registry":
			n.defaultRegistry = value
		case strings.HasPrefix(key, "@") && strings.HasSuffix(key, ":registry"):
			scope := strings.TrimSuffix(key, ":registry")
			n.scopeRegistry[scope] = value
		case strings.HasPrefix(key, "//") && strings.HasSuffix(key, ":_authToken"):
			prefix := normalizeRegistryKey(strings.TrimSuffix(key, ":_authToken"))
			n.authTokens[prefix] = value
		}
	}
}

// RegistryFor returns the registry URL to use for a package: the scope's
// registry when the package is scoped and configured, otherwise the default.
func (n *NPMRC) RegistryFor(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		scope := pkg
		if i := strings.IndexByte(pkg, '/'); i >= 0 {
			scope = pkg[:i]
		}
		if r, ok := n.scopeRegistry[scope]; ok && r != "" {
			return r
		}
	}
	if n.defaultRegistry == "" {
		return DefaultRegistry
	}
	return n.defaultRegistry
}

// AuthToken returns the auth token configured for the registry (or tarball) URL,
// using npm's longest-prefix match over the scheme-stripped "//host/path/"
// keys. Returns "" when no token applies.
func (n *NPMRC) AuthToken(rawURL string) string {
	target := normalizeRegistryKey(rawURL)
	var best string
	var bestLen int
	for key, tok := range n.authTokens {
		if strings.HasPrefix(target, key) && len(key) > bestLen {
			best, bestLen = tok, len(key)
		}
	}
	return best
}

// normalizeRegistryKey strips the scheme from a registry URL and guarantees a
// trailing slash, yielding npm's "//host/path/" auth-key form.
func normalizeRegistryKey(u string) string {
	for _, scheme := range []string{"https:", "http:"} {
		if strings.HasPrefix(u, scheme) {
			u = strings.TrimPrefix(u, scheme)
			break
		}
	}
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u
}

// unquote removes a single pair of surrounding single or double quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// expandEnv replaces ${VAR} references with the corresponding environment
// variable, matching npm's interpolation of secrets out of the environment.
func expandEnv(s string) string {
	return envRefRe.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(m[2 : len(m)-1])
	})
}
