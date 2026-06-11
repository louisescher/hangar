package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/louisescher/hangar/internal/fsutil"
)

// View modes for the skill picker.
const (
	ViewTree = "tree"
	ViewList = "list"
)

// Install scopes.
const (
	ScopeLocal  = "local"
	ScopeGlobal = "global"
)

// Prefs are the persisted user preferences, loaded at startup and written when
// changed in the TUI. CLI flags always override the persisted value for a run.
type Prefs struct {
	View           string   `toml:"view"`            // ViewTree | ViewList
	DefaultAgents  []string `toml:"default_agents"`  // pre-checked target agents
	Scope          string   `toml:"scope"`           // ScopeLocal | ScopeGlobal
	StripComments  bool     `toml:"strip_comments"`  // sanitize references' comments
	StripInvisible bool     `toml:"strip_invisible"` // sanitize invisible characters
}

// DefaultPrefs returns the built-in defaults used when no config file exists.
func DefaultPrefs() Prefs {
	return Prefs{
		View:           ViewTree,
		Scope:          ScopeLocal,
		StripComments:  true,
		StripInvisible: true,
	}
}

// ConfigDir returns Hangar's configuration directory, honoring $XDG_CONFIG_HOME
// and falling back to ~/.config/hangar.
func ConfigDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "hangar"), nil
}

// PrefsPath returns the config file location.
func PrefsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// LoadPrefs reads the config file, returning defaults when it is missing.
// Unspecified fields keep their default values.
func LoadPrefs() (Prefs, error) {
	p := DefaultPrefs()
	path, err := PrefsPath()
	if err != nil {
		return p, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return p, err
	}
	if err := toml.Unmarshal(data, &p); err != nil {
		return DefaultPrefs(), fmt.Errorf("parse %s: %w", path, err)
	}
	p.normalize()
	return p, nil
}

// Save writes the preferences atomically to the config path.
func (p Prefs) Save() error {
	p.normalize()
	path, err := PrefsPath()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# hangar preferences\n")
	if err := toml.NewEncoder(&buf).Encode(p); err != nil {
		return fmt.Errorf("encode prefs: %w", err)
	}
	return fsutil.AtomicWriteFile(path, buf.Bytes(), 0o644)
}

// normalize coerces unknown enum values back to their defaults.
func (p *Prefs) normalize() {
	if p.View != ViewTree && p.View != ViewList {
		p.View = ViewTree
	}
	if p.Scope != ScopeLocal && p.Scope != ScopeGlobal {
		p.Scope = ScopeLocal
	}
}
