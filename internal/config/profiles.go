package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/louisescher/hangar/internal/fsutil"
	"github.com/louisescher/hangar/internal/lockfile"
)

// Profile is a named, portable set of installed entries that can be re-applied
// to another project.
type Profile struct {
	Name   string           `toml:"name"`
	Skills []lockfile.Entry `toml:"skills"`
}

var profileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ProfilesDir returns the directory holding saved profiles.
func ProfilesDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "profiles"), nil
}

func profilePath(name string) (string, error) {
	if !profileNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid profile name %q (use letters, digits, '.', '_', '-')", name)
	}
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".toml"), nil
}

// SaveProfile writes a profile atomically.
func SaveProfile(p Profile) error {
	path, err := profilePath(p.Name)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# hangar profile\n")
	if err := toml.NewEncoder(&buf).Encode(p); err != nil {
		return fmt.Errorf("encode profile: %w", err)
	}
	return fsutil.AtomicWriteFile(path, buf.Bytes(), 0o644)
}

// LoadProfile reads a saved profile by name.
func LoadProfile(name string) (Profile, error) {
	path, err := profilePath(name)
	if err != nil {
		return Profile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, fmt.Errorf("no profile named %q", name)
		}
		return Profile{}, err
	}
	var p Profile
	if err := toml.Unmarshal(data, &p); err != nil {
		return Profile{}, fmt.Errorf("parse profile %q: %w", name, err)
	}
	if p.Name == "" {
		p.Name = name
	}
	return p, nil
}

// ListProfiles returns the names of all saved profiles, sorted.
func ListProfiles() ([]string, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
	}
	sort.Strings(names)
	return names, nil
}

// RemoveProfile deletes a saved profile, reporting whether it existed.
func RemoveProfile(name string) (bool, error) {
	path, err := profilePath(name)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
