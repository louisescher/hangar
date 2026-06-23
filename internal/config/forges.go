package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// HostConfig registers a self-hosted git forge: its dialect ("type") and an
// optional access token. The token may reference an environment variable with
// ${VAR}, matching the .npmrc convention.
type HostConfig struct {
	Type  string `toml:"type"`  // gitlab | forgejo | gitea | bitbucket | generic
	Token string `toml:"token"` // ${ENV}-expanded on load
}

// Forges is the [forges] table of the Hangar config file: per-provider tokens
// for the public hosts and a registry of self-hosted hosts.
type Forges struct {
	GitHubToken    string                `toml:"github_token"`
	GitLabToken    string                `toml:"gitlab_token"`
	ForgejoToken   string                `toml:"forgejo_token"`
	BitbucketToken string                `toml:"bitbucket_token"`
	Hosts          map[string]HostConfig `toml:"hosts"`
}

// forgesFile decodes just the [forges] table, ignoring the rest of config.toml.
type forgesFile struct {
	Forges Forges `toml:"forges"`
}

// LoadForges reads and merges forge configuration. Precedence, low to high:
// ~/.config/hangar/config.toml, then ./hangar.toml in the working directory, so
// a project-local file overrides the user's. Missing files are ignored. Token
// values have ${ENV} references expanded.
func LoadForges() *Forges {
	f := &Forges{Hosts: map[string]HostConfig{}}
	for _, path := range forgeConfigFiles() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var ff forgesFile
		if _, err := toml.Decode(string(data), &ff); err != nil {
			continue
		}
		f.merge(ff.Forges)
	}
	f.expand()
	return f
}

func forgeConfigFiles() []string {
	var files []string
	if dir, err := ConfigDir(); err == nil {
		files = append(files, filepath.Join(dir, "config.toml"))
	}
	if wd, err := os.Getwd(); err == nil {
		files = append(files, filepath.Join(wd, "hangar.toml"))
	}
	return files
}

// merge overlays non-empty fields of o onto f (later files win).
func (f *Forges) merge(o Forges) {
	if o.GitHubToken != "" {
		f.GitHubToken = o.GitHubToken
	}
	if o.GitLabToken != "" {
		f.GitLabToken = o.GitLabToken
	}
	if o.ForgejoToken != "" {
		f.ForgejoToken = o.ForgejoToken
	}
	if o.BitbucketToken != "" {
		f.BitbucketToken = o.BitbucketToken
	}
	for host, hc := range o.Hosts {
		f.Hosts[strings.ToLower(host)] = hc
	}
}

// expand resolves ${ENV} references in all token values.
func (f *Forges) expand() {
	f.GitHubToken = expandEnv(f.GitHubToken)
	f.GitLabToken = expandEnv(f.GitLabToken)
	f.ForgejoToken = expandEnv(f.ForgejoToken)
	f.BitbucketToken = expandEnv(f.BitbucketToken)
	for host, hc := range f.Hosts {
		hc.Token = expandEnv(hc.Token)
		f.Hosts[host] = hc
	}
}

// SelfHostedForges returns the host→type mapping for registered self-hosted
// hosts (lowercased hosts, type strings as written in the config). The public
// hosts (github.com, gitlab.com, …) are not included; they are built in.
func (f *Forges) SelfHostedForges() map[string]string {
	out := make(map[string]string, len(f.Hosts))
	for host, hc := range f.Hosts {
		if hc.Type != "" {
			out[strings.ToLower(host)] = hc.Type
		}
	}
	return out
}

// forgeEnvVars lists the environment variables consulted for each forge, in
// precedence order.
var forgeEnvVars = map[string][]string{
	"github":    {"GH_TOKEN", "GITHUB_TOKEN"},
	"gitlab":    {"GITLAB_TOKEN"},
	"forgejo":   {"FORGEJO_TOKEN", "GITEA_TOKEN"},
	"gitea":     {"FORGEJO_TOKEN", "GITEA_TOKEN"},
	"bitbucket": {"BITBUCKET_TOKEN"},
}

// TokenFor resolves the access token for a forge dialect and host. Precedence:
// the provider's environment variable(s), then a host-specific config token,
// then the provider-level config token. Returns "" when none is set
// (unauthenticated access). host is the bare hostname (no scheme).
func (f *Forges) TokenFor(forge, host string) string {
	forge = strings.ToLower(forge)
	for _, env := range forgeEnvVars[forge] {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	if hc, ok := f.Hosts[strings.ToLower(host)]; ok && hc.Token != "" {
		return hc.Token
	}
	switch forge {
	case "github":
		return f.GitHubToken
	case "gitlab":
		return f.GitLabToken
	case "forgejo", "gitea":
		return f.ForgejoToken
	case "bitbucket":
		return f.BitbucketToken
	}
	return ""
}
