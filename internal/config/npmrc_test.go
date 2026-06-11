package config

import "testing"

func TestNPMRCDefaultRegistry(t *testing.T) {
	rc := ParseNPMRC("")
	if got := rc.RegistryFor("lodash"); got != DefaultRegistry {
		t.Errorf("default registry = %q, want %q", got, DefaultRegistry)
	}
}

func TestNPMRCScopedRegistry(t *testing.T) {
	rc := ParseNPMRC("registry=https://corp.example/npm/\n@acme:registry=https://acme.example/npm/\n")

	if got := rc.RegistryFor("@acme/widgets"); got != "https://acme.example/npm/" {
		t.Errorf("scoped registry = %q", got)
	}
	// An unconfigured scope falls back to the default registry.
	if got := rc.RegistryFor("@other/thing"); got != "https://corp.example/npm/" {
		t.Errorf("fallback registry = %q", got)
	}
	if got := rc.RegistryFor("lodash"); got != "https://corp.example/npm/" {
		t.Errorf("unscoped registry = %q", got)
	}
}

func TestNPMRCAuthTokenLongestPrefix(t *testing.T) {
	rc := ParseNPMRC(
		"//registry.npmjs.org/:_authToken=public\n" +
			"//npm.pkg.github.com/:_authToken=ghtoken\n",
	)

	if got := rc.AuthToken("https://npm.pkg.github.com/@acme/widgets"); got != "ghtoken" {
		t.Errorf("github registry token = %q, want ghtoken", got)
	}
	if got := rc.AuthToken("https://registry.npmjs.org/"); got != "public" {
		t.Errorf("npmjs token = %q, want public", got)
	}
	if got := rc.AuthToken("https://unknown.example/"); got != "" {
		t.Errorf("unknown registry should have no token, got %q", got)
	}
}

func TestNPMRCEnvExpansionAndQuotes(t *testing.T) {
	t.Setenv("HANGAR_TEST_NPM_TOKEN", "s3cret")
	rc := ParseNPMRC("//registry.npmjs.org/:_authToken=\"${HANGAR_TEST_NPM_TOKEN}\"\n")
	if got := rc.AuthToken("https://registry.npmjs.org/"); got != "s3cret" {
		t.Errorf("expanded token = %q, want s3cret", got)
	}
}

func TestNPMRCCommentsAndBlankLines(t *testing.T) {
	rc := ParseNPMRC("# a comment\n; another\n\nregistry=https://corp.example/\n")
	if got := rc.RegistryFor("x"); got != "https://corp.example/" {
		t.Errorf("registry = %q", got)
	}
}

func TestNPMRCLaterFileOverrides(t *testing.T) {
	// parse is additive: a second call (a higher-precedence file) overrides.
	rc := defaultNPMRC()
	rc.parse("registry=https://user.example/\n")
	rc.parse("registry=https://project.example/\n")
	if got := rc.RegistryFor("x"); got != "https://project.example/" {
		t.Errorf("override registry = %q, want project.example", got)
	}
}
