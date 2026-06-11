// Package link materializes a skill into an agent's skills directory: a
// relative symlink for local (project) installs, a full copy for global installs.
package link

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/louisescher/hangar/internal/fsutil"
)

// Symlink creates a relative symlink at linkPath pointing to canonicalPath.
// An existing symlink at linkPath is replaced; an existing real file or
// directory is left untouched and reported as an error (non-fatal to the
// caller, which records it as a failed agent).
func Symlink(canonicalPath, linkPath string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(linkPath); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a symlink; leaving it untouched", linkPath)
		}
		if err := os.Remove(linkPath); err != nil {
			return err
		}
	}
	rel, err := filepath.Rel(filepath.Dir(linkPath), canonicalPath)
	if err != nil {
		// Fall back to an absolute target if a relative one can't be computed.
		rel = canonicalPath
	}
	return os.Symlink(rel, linkPath)
}

// Copy replaces dst with a fresh copy of the canonical skill directory at src.
func Copy(src, dst string) error {
	if fsutil.Exists(dst) {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}
	return fsutil.CopyDir(src, dst)
}
