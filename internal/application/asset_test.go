package application

import (
	"os"
	"path/filepath"
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

	a := newAssetExistence(root)

	if !a.AssetExists("logo.png") {
		t.Error("AssetExists(logo.png) = false, want true (in-root regular file)")
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
