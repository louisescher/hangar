package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadForges(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("COMPANY_TOKEN", "from-env-ref")
	hangarDir := filepath.Join(dir, "hangar")
	if err := os.MkdirAll(hangarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `
[forges]
gitlab_token = "gl-token"
bitbucket_token = "bb-token"

[forges.hosts."git.company.com"]
type = "gitlab"
token = "${COMPANY_TOKEN}"

[forges.hosts."Gitea.Example.NET"]
type = "gitea"
token = "gitea-token"
`
	if err := os.WriteFile(filepath.Join(hangarDir, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// Avoid a stray ./hangar.toml in the working dir affecting the merge.
	wd := t.TempDir()
	chdir(t, wd)

	f := LoadForges()
	if f.GitLabToken != "gl-token" || f.BitbucketToken != "bb-token" {
		t.Errorf("provider tokens = %q/%q", f.GitLabToken, f.BitbucketToken)
	}
	hc, ok := f.Hosts["git.company.com"]
	if !ok || hc.Type != "gitlab" || hc.Token != "from-env-ref" {
		t.Errorf("git.company.com = %+v, ok=%v (want gitlab/from-env-ref)", hc, ok)
	}
	// Host keys are lowercased.
	if _, ok := f.Hosts["gitea.example.net"]; !ok {
		t.Errorf("expected lowercased host key gitea.example.net; hosts=%v", f.Hosts)
	}

	sh := f.SelfHostedForges()
	if sh["git.company.com"] != "gitlab" || sh["gitea.example.net"] != "gitea" {
		t.Errorf("SelfHostedForges = %v", sh)
	}
}

func TestTokenForEnvWinsOverConfig(t *testing.T) {
	f := &Forges{
		GitLabToken: "config-gl",
		Hosts: map[string]HostConfig{
			"git.company.com": {Type: "gitlab", Token: "config-host"},
		},
	}

	// No env: provider config token, then host config token.
	if got := f.TokenFor("gitlab", "gitlab.com"); got != "config-gl" {
		t.Errorf("gitlab.com token = %q, want config-gl", got)
	}
	if got := f.TokenFor("gitlab", "git.company.com"); got != "config-host" {
		t.Errorf("self-hosted token = %q, want config-host (host-specific over provider)", got)
	}

	// Env wins for the provider, even on a self-hosted host of that dialect.
	t.Setenv("GITLAB_TOKEN", "env-gl")
	if got := f.TokenFor("gitlab", "gitlab.com"); got != "env-gl" {
		t.Errorf("gitlab.com token = %q, want env-gl", got)
	}
	if got := f.TokenFor("gitlab", "git.company.com"); got != "env-gl" {
		t.Errorf("self-hosted token = %q, want env-gl (env wins)", got)
	}

	// Unset forge with no config and no env yields "".
	if got := f.TokenFor("bitbucket", "bitbucket.org"); got != "" {
		t.Errorf("bitbucket token = %q, want empty", got)
	}
}

// chdir changes to dir for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}
