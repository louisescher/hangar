// Package config resolves environment- and machine-level configuration:
// credentials, registry settings, and persisted user preferences.
package config

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GitHubToken resolves a GitHub token from, in order: $GH_TOKEN, $GITHUB_TOKEN,
// then `gh auth token`. Returns "" when none is available (unauthenticated
// access, subject to GitHub's lower rate limits).
func GitHubToken() string {
	for _, env := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return ghCLIToken()
}

func ghCLIToken() string {
	if _, err := exec.LookPath("gh"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
