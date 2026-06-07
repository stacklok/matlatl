package application

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFSAssetExistence exercises the security guard on the default (non-integration)
// test path: a real in-root asset exists, a "../" traversal is refused without a
// stat, a missing file is absent, and an empty root disables checks.
func TestFSAssetExistence(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "logo.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A sibling file OUTSIDE the root that a traversal would try to reach.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A real in-root directory: an existing non-markdown directory is an asset.
	if err := os.Mkdir(filepath.Join(root, "examples"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := newAssetExistence(root)

	if !a.AssetExists("logo.png") {
		t.Error("AssetExists(logo.png) = false, want true (in-root regular file)")
	}
	if !a.AssetExists("examples") {
		t.Error("AssetExists(examples) = false, want true (in-root directory)")
	}
	if a.AssetExists("missing.png") {
		t.Error("AssetExists(missing.png) = true, want false")
	}

	// A crafted relative traversal must be refused by the guard, never reaching
	// the outside file — even though that file exists.
	rel, err := filepath.Rel(root, filepath.Join(outside, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if a.AssetExists(filepath.ToSlash(rel)) {
		t.Errorf("AssetExists(%q) = true; the ../-escape guard failed", rel)
	}
	if a.AssetExists("../secret") {
		t.Error("AssetExists(../secret) = true; guard failed")
	}
	if a.AssetExists("") {
		t.Error("AssetExists(\"\") = true, want false")
	}

	// An empty root disables asset checks entirely.
	empty := &fsAssetExistence{}
	if empty.AssetExists("logo.png") {
		t.Error("empty-root AssetExists should always be false")
	}
}

// TestFSAssetExistence_SymlinkExcluded pins the load-bearing safety property:
// AssetExists uses Lstat, so a symlink — whether it points at an in-root
// directory OR an in-root regular file — is neither a regular file nor a
// directory and must never count as an asset (ADR 0003 containment).
func TestFSAssetExistence_SymlinkExcluded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "examples"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logo.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "examples"), filepath.Join(root, "dirlink")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "logo.png"), filepath.Join(root, "filelink")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	a := newAssetExistence(root)
	if a.AssetExists("dirlink") {
		t.Error("AssetExists(dirlink) = true; a symlink-to-directory must never count as an asset")
	}
	if a.AssetExists("filelink") {
		t.Error("AssetExists(filelink) = true; a symlink-to-file must never count as an asset")
	}
}
