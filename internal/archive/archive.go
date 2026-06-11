// Package archive extracts gzip-compressed tar archives (e.g. GitHub codeload
// tarballs) to disk, defending against path-traversal ("zip-slip").
package archive

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/louisescher/hangar/internal/fsutil"
)

// maxFileBytes caps a single extracted file to guard against decompression
// bombs. Skills and docs are small; 256 MiB is a generous ceiling.
const maxFileBytes = 256 << 20

// ExtractTarGz extracts the gzip-compressed tar stream r into dest. Entries that
// would escape dest are rejected. Symlinks and other special files are skipped.
func ExtractTarGz(r io.Reader, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		// Reject absolute paths and any traversal before touching disk.
		clean := filepath.Clean(hdr.Name)
		if filepath.IsAbs(clean) {
			return fmt.Errorf("archive entry %q is absolute", hdr.Name)
		}
		target := filepath.Join(dest, clean)
		if err := fsutil.MustWithinRoot(dest, target); err != nil {
			return fmt.Errorf("archive entry %q escapes destination: %w", hdr.Name, err)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeReg(tr, target, os.FileMode(hdr.Mode).Perm()); err != nil {
				return err
			}
		default:
			// Skip symlinks, hardlinks, devices, etc. for safety.
			continue
		}
	}
}

func writeReg(tr *tar.Reader, target string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	// LimitReader guards against a malicious uncompressed size.
	if _, err := io.Copy(f, io.LimitReader(tr, maxFileBytes)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// SingleRoot returns the path to the sole subdirectory of dir when there is
// exactly one (the usual shape of a GitHub tarball: "<repo>-<ref>/"). Otherwise
// it returns dir unchanged.
func SingleRoot(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		} else {
			// A stray file at the top level means it isn't a clean single root.
			return dir, nil
		}
	}
	if len(dirs) == 1 {
		return filepath.Join(dir, dirs[0]), nil
	}
	return dir, nil
}
