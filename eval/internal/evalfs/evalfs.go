// Package evalfs provides root-confined filesystem operations for the offline
// evaluation harness. These guards are not hostile-process sandboxing.
package evalfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

const (
	// MaxFileBytes bounds files read by the evaluation harness.
	MaxFileBytes = 1 << 20
	// MaxFiles bounds files enumerated beneath one root.
	MaxFiles = 4096
)

// Root returns a canonical absolute directory path. The root entry itself must
// not be a symlink. Symlinks in its absolute parent path are canonicalized
// before os.OpenRoot (notably macOS /var -> /private/var); symlinks below the
// resulting caller-controlled root are still rejected by every operation.
func Root(name string) (string, error) {
	abs, err := resolveRoot(name)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return "", fmt.Errorf("evalfs: open root: %w", err)
	}
	if err := root.Close(); err != nil {
		return "", fmt.Errorf("evalfs: close root: %w", err)
	}
	return abs, nil
}

func resolveRoot(name string) (string, error) {
	if name == "" {
		return "", errors.New("evalfs: empty root")
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", fmt.Errorf("evalfs: resolve root: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("evalfs: stat root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("evalfs: root must not be a symlink: %q", name)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("evalfs: root is not a directory: %q", name)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("evalfs: canonicalize root: %w", err)
	}
	return canonical, nil
}

// Path returns an absolute child path for APIs that require one (the existing
// matlatl pipeline and report destination). os.Root intentionally exposes no
// absolute child path, so this lexical join remains; all actual eval file I/O
// below uses os.Root. Existing symlink components are rejected first.
func Path(absRoot, rel string) (string, error) {
	name, err := localName(rel)
	if err != nil {
		return "", err
	}
	root, resolved, err := openRoot(absRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	if err := rejectSymlinks(root, name, true); err != nil {
		return "", err
	}
	return filepath.Join(resolved, name), nil
}

func localName(rel string) (string, error) {
	name := filepath.Clean(filepath.FromSlash(rel))
	if name == "." || !filepath.IsLocal(name) {
		return "", fmt.Errorf("evalfs: unsafe relative path %q", rel)
	}
	return name, nil
}

func openRoot(name string) (*os.Root, string, error) {
	abs, err := resolveRoot(name)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, "", fmt.Errorf("evalfs: open root: %w", err)
	}
	return root, abs, nil
}

func rejectSymlinks(root *os.Root, name string, allowMissing bool) error {
	current := ""
	for _, part := range splitPath(name) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if allowMissing && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("evalfs: stat %q: %w", name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("evalfs: symlink rejected in %q", name)
		}
	}
	return nil
}

func splitPath(name string) []string {
	var parts []string
	for name != "." && name != string(filepath.Separator) && name != "" {
		dir, base := filepath.Split(name)
		parts = append(parts, base)
		name = filepath.Clean(dir)
	}
	slices.Reverse(parts)
	return parts
}

// Files returns all regular files below absRoot as sorted slash-separated
// relative paths. Symlinks and special files are rejected, not skipped.
func Files(absRoot string) ([]string, error) {
	root, _, err := openRoot(absRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	var paths []string
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("evalfs: symlink rejected: %q", name)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("evalfs: non-regular file rejected: %q", name)
		}
		if info.Size() > MaxFileBytes {
			return fmt.Errorf("evalfs: file exceeds %d bytes: %q", MaxFileBytes, name)
		}
		if len(paths) == MaxFiles {
			return fmt.Errorf("evalfs: file count exceeds %d", MaxFiles)
		}
		paths = append(paths, filepath.ToSlash(name))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	return paths, nil
}

// Read reads a bounded regular file beneath absRoot.
func Read(absRoot, rel string) ([]byte, error) {
	name, err := localName(rel)
	if err != nil {
		return nil, err
	}
	root, _, err := openRoot(absRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if err := rejectSymlinks(root, name, false); err != nil {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("evalfs: read regular file %q: %w", rel, err)
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("evalfs: file exceeds %d bytes: %q", MaxFileBytes, rel)
	}
	content, err := root.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("evalfs: read %q: %w", rel, err)
	}
	return content, nil
}

// FileHash returns the SHA-256 hash of a confined file.
func FileHash(absRoot, rel string) (string, error) {
	content, err := Read(absRoot, rel)
	if err != nil {
		return "", err
	}
	return digest(content), nil
}

// TreeHash deterministically hashes sorted relative paths and file contents.
func TreeHash(absRoot string) (string, error) {
	files, err := Files(absRoot)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, rel := range files {
		content, err := Read(absRoot, rel)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%d:%s:%d:", len(rel), rel, len(content))
		_, _ = hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Chmod changes a root-confined existing file or directory's permissions.
func Chmod(absRoot, rel string, mode fs.FileMode) error {
	name, err := localName(rel)
	if err != nil {
		return err
	}
	root, _, err := openRoot(absRoot)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := rejectSymlinks(root, name, false); err != nil {
		return err
	}
	if err := root.Chmod(name, mode); err != nil {
		return fmt.Errorf("evalfs: chmod %q: %w", rel, err)
	}
	return nil
}

// WriteExclusive creates a root-confined file and refuses overwrite.
func WriteExclusive(absRoot, rel string, content []byte) error {
	name, err := localName(rel)
	if err != nil {
		return err
	}
	root, _, err := openRoot(absRoot)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	parent := filepath.Dir(name)
	if parent != "." {
		if err := rejectSymlinks(root, parent, true); err != nil {
			return err
		}
		if err := root.MkdirAll(parent, 0o750); err != nil {
			return fmt.Errorf("evalfs: create parent: %w", err)
		}
		if err := rejectSymlinks(root, parent, false); err != nil {
			return err
		}
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("evalfs: exclusive create %q: %w", rel, err)
	}
	if _, err = file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("evalfs: write %q: %w", rel, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("evalfs: close %q: %w", rel, err)
	}
	return nil
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
