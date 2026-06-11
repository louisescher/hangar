// Package fsutil provides small filesystem helpers shared across Hangar:
// atomic file writes and recursive directory copies.
package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// AtomicWriteFile writes data to path atomically: it writes to a temporary file
// in the same directory, fsyncs it, then renames it over the destination. This
// guarantees readers never observe a partially written file.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".hangar-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
func IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// CopyFile copies a single regular file from src to dst, creating parent
// directories as needed and preserving the source file mode.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// CopyDir recursively copies the directory tree rooted at src into dst.
// Symbolic links are skipped: a skill's canonical copy should contain only
// real files, and skipping links avoids copying anything outside the tree.
func CopyDir(src, dst string) error {
	root, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		case d.Type()&os.ModeSymlink != 0:
			return nil // skip symlinks
		case d.Type().IsRegular():
			return CopyFile(path, target)
		default:
			return nil // skip devices, sockets, pipes, etc.
		}
	})
}

// WithinRoot reports whether the cleaned child path stays inside root. It is
// the shared containment check used to defend against archive ("zip-slip") and
// symlink traversal.
func WithinRoot(root, child string) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	child, err = filepath.Abs(child)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

// MustWithinRoot returns an error if child escapes root.
func MustWithinRoot(root, child string) error {
	ok, err := WithinRoot(root, child)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("path %q escapes root %q", child, root)
	}
	return nil
}
