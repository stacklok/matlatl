package evalfs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSafeFilesystem(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := Files(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(files, []string{"a/a.txt", "b.txt"}) {
		t.Fatalf("files = %v", files)
	}
	hash1, err := TreeHash(root)
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := TreeHash(root)
	if err != nil || hash1 != hash2 {
		t.Fatalf("tree hash not deterministic: %q %q, %v", hash1, hash2, err)
	}
	for _, unsafe := range []string{"../escape", filepath.Join(root, "absolute"), "../" + filepath.Base(root) + "-prefix"} {
		if _, err := Path(root, unsafe); err == nil {
			t.Errorf("Path accepted %q", unsafe)
		}
		if err := WriteExclusive(root, unsafe, []byte("escape")); err == nil {
			t.Errorf("WriteExclusive accepted %q", unsafe)
		}
	}
	if err := WriteExclusive(root, "records/one.json", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteExclusive(root, "records/one.json", []byte("two")); err == nil {
		t.Fatal("exclusive write overwrote a record")
	}
}

func TestRejectInRootSymlinkAndSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(root, "inside-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Read(root, "inside-link"); err == nil {
		t.Fatal("Read followed an in-root symlink")
	}
	if err := WriteExclusive(root, "inside-link/child", []byte("x")); err == nil {
		t.Fatal("WriteExclusive followed an in-root symlink")
	}

	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Root(alias); err == nil {
		t.Fatal("Root accepted a symlink component")
	}
}

func TestRejectSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Path(root, "link/file"); err == nil {
		t.Fatal("Path accepted symlink")
	}
	if _, err := Files(root); err == nil {
		t.Fatal("Files accepted symlink")
	}
}
