package gitforge

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/louisescher/hangar/internal/spec"
)

// GitHubProfile fetches from github.com: ref advertisement on github.com, the
// tarball from codeload.github.com, and HTTP Basic "x-access-token:<token>".
func GitHubProfile() Profile {
	return Profile{
		Name:           "github",
		BaseURL:        "https://github.com",
		TarballBaseURL: "https://codeload.github.com",
		TarballPath: func(owner, repo, ref string) string {
			return fmt.Sprintf("/%s/%s/tar.gz/%s", owner, repo, ref)
		},
		Auth: basicAuth("x-access-token"),
	}
}

// GitLabProfile fetches from a GitLab host (host is a full origin, e.g.
// https://gitlab.com). Auth uses the PRIVATE-TOKEN header.
func GitLabProfile(host string) Profile {
	return Profile{
		Name:           "gitlab",
		BaseURL:        host,
		TarballBaseURL: host,
		TarballPath: func(owner, repo, ref string) string {
			return fmt.Sprintf("/%s/%s/-/archive/%s/%s-%s.tar.gz", owner, repo, ref, repo, ref)
		},
		Auth: headerAuth("PRIVATE-TOKEN"),
	}
}

// ForgejoProfile fetches from a Forgejo/Gitea host (covers Codeberg). Auth uses
// the "Authorization: token <token>" header.
func ForgejoProfile(host string) Profile {
	return Profile{
		Name:           "forgejo",
		BaseURL:        host,
		TarballBaseURL: host,
		TarballPath: func(owner, repo, ref string) string {
			return fmt.Sprintf("/%s/%s/archive/%s.tar.gz", owner, repo, ref)
		},
		Auth: tokenAuth(),
	}
}

// BitbucketProfile fetches from a Bitbucket Cloud host. Auth uses HTTP Basic
// "x-token-auth:<token>" (Bitbucket accepts an access token as the password).
func BitbucketProfile(host string) Profile {
	return Profile{
		Name:           "bitbucket",
		BaseURL:        host,
		TarballBaseURL: host,
		TarballPath: func(owner, repo, ref string) string {
			return fmt.Sprintf("/%s/%s/get/%s.tar.gz", owner, repo, ref)
		},
		Auth: basicAuth("x-token-auth"),
	}
}

// TangledProfile fetches from a Tangled host (tangled.org or its tangled.sh
// alias). Its archive layout matches Forgejo/Gitea; Tangled has no token auth.
func TangledProfile(host string) Profile {
	return Profile{
		Name:           "tangled",
		BaseURL:        host,
		TarballBaseURL: host,
		TarballPath: func(owner, repo, ref string) string {
			return fmt.Sprintf("/%s/%s/archive/%s.tar.gz", owner, repo, ref)
		},
	}
}

// GenericProfile is the catch-all for self-hosted hosts of unknown dialect. It
// assumes the Forgejo/Gitea archive layout, which is the most common among
// self-hosted open-source forges.
func GenericProfile(host string) Profile {
	p := ForgejoProfile(host)
	p.Name = "generic"
	return p
}

// ProfileFor returns the profile for a forge dialect and host origin.
func ProfileFor(forge spec.Forge, host string) (Profile, error) {
	switch forge {
	case spec.ForgeGitHub:
		return GitHubProfile(), nil
	case spec.ForgeGitLab:
		return GitLabProfile(host), nil
	case spec.ForgeForgejo:
		return ForgejoProfile(host), nil
	case spec.ForgeBitbucket:
		return BitbucketProfile(host), nil
	case spec.ForgeTangled:
		return TangledProfile(host), nil
	case spec.ForgeGeneric:
		return GenericProfile(host), nil
	default:
		return Profile{}, fmt.Errorf("unsupported forge %q", forge)
	}
}

// basicAuth returns an Auth func that sets HTTP Basic "<user>:<token>".
func basicAuth(user string) func(*http.Request, string) {
	return func(req *http.Request, token string) {
		if token == "" {
			return
		}
		cred := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
		req.Header.Set("Authorization", "Basic "+cred)
	}
}

// headerAuth returns an Auth func that sets a bare token header (e.g.
// PRIVATE-TOKEN for GitLab).
func headerAuth(name string) func(*http.Request, string) {
	return func(req *http.Request, token string) {
		if token == "" {
			return
		}
		req.Header.Set(name, token)
	}
}

// tokenAuth returns an Auth func that sets "Authorization: token <token>"
// (Forgejo/Gitea).
func tokenAuth() func(*http.Request, string) {
	return func(req *http.Request, token string) {
		if token == "" {
			return
		}
		req.Header.Set("Authorization", "token "+token)
	}
}
