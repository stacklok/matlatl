package fsscanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, root string, cfg Config) application.ScanResult {
	t.Helper()
	res, err := New(cfg).Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	return res
}

func ids(res application.ScanResult) []string {
	out := make([]string, 0, len(res.Files))
	for _, f := range res.Files {
		out = append(out, f.ID.String())
	}
	return out
}

func hasNotice(res application.ScanResult, kind application.NoticeKind) bool {
	return countNotice(res, kind) > 0
}

func countNotice(res application.ScanResult, kind application.NoticeKind) int {
	n := 0
	for _, notice := range res.Notices {
		if notice.Kind == kind {
			n++
		}
	}
	return n
}

func noticeFor(res application.ScanResult, kind application.NoticeKind) (application.Notice, bool) {
	for _, n := range res.Notices {
		if n.Kind == kind {
			return n, true
		}
	}
	return application.Notice{}, false
}

func TestScan_DiscoversMarkdownSortedAndDistinctIDs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, "docs", "README.md"), "# Docs") // duplicate basename
	writeFile(t, filepath.Join(root, "docs", "guide.markdown"), "# Guide")
	writeFile(t, filepath.Join(root, "notes.txt"), "not markdown")

	res := scan(t, root, Config{})
	got := ids(res)
	want := []string{"README.md", "docs/README.md", "docs/guide.markdown"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want sorted %v", got, want)
	}
	// Duplicate basenames are distinct DocumentIDs (ADR 0001).
	if got[0] == got[1] {
		t.Error("duplicate basenames collapsed to one ID")
	}
}

func TestScan_DefaultIgnoresAndIgnoreFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")
	writeFile(t, filepath.Join(root, ".git", "config.md"), "# git")
	writeFile(t, filepath.Join(root, "node_modules", "pkg.md"), "# nm")
	writeFile(t, filepath.Join(root, "vendor", "v.md"), "# vendor")
	writeFile(t, filepath.Join(root, "ignored", "x.md"), "# ignored")
	writeFile(t, filepath.Join(root, "draft-foo.md"), "# draft")
	writeFile(t, filepath.Join(root, ".doctopusignore"), "ignored/\ndraft-*.md\n")

	res := scan(t, root, Config{})
	got := ids(res)
	if len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want only [keep.md] (defaults + .doctopusignore honored)", got)
	}
}

func TestScan_OutputDirExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")
	out := filepath.Join(root, "out")
	writeFile(t, filepath.Join(out, "artifact.md"), "# artifact")

	res := scan(t, root, Config{OutputDir: out})
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want [keep.md] (output dir excluded)", got)
	}
}

func TestScan_OversizedFileSkippedAndNoticed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "small.md"), "# ok")
	writeFile(t, filepath.Join(root, "big.md"), strings.Repeat("x", 5000))

	res := scan(t, root, Config{MaxFileSizeBytes: 1000})
	if got := ids(res); len(got) != 1 || got[0] != "small.md" {
		t.Errorf("ids = %v, want [small.md] (oversized skipped)", got)
	}
	if !hasNotice(res, application.NoticeOversized) {
		t.Error("expected an oversized notice")
	}
}

func TestScan_MaxFilesTruncationReported(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a.md", "b.md", "c.md", "d.md"} {
		writeFile(t, filepath.Join(root, n), "# "+n)
	}
	res := scan(t, root, Config{MaxFiles: 2})
	if len(res.Files) != 2 {
		t.Errorf("discovered %d files, want capped at 2", len(res.Files))
	}
	if !hasNotice(res, application.NoticeTruncated) {
		t.Error("expected a truncation notice (no silent cap)")
	}
}

func TestScan_SymlinkToOutsideRootSkippedAndReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.md"), "# secret")
	writeFile(t, filepath.Join(root, "real.md"), "# real")

	link := filepath.Join(root, "escape.md")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "real.md" {
		t.Errorf("ids = %v, want only [real.md]; the escaping symlink must not be read", got)
	}
	// The escaping symlink is caught by the no-follow policy: it must be a
	// skipped-symlink notice (specifically), not misclassified as another kind.
	n, ok := noticeFor(res, application.NoticeSkippedSymlink)
	if !ok {
		t.Fatal("expected a skipped-symlink notice for the escaping link")
	}
	if !strings.Contains(n.Detail, "escapes the scan root") {
		t.Errorf("escaping-symlink detail = %q, want it to mention escaping the root", n.Detail)
	}
	// No genuine root-escape notice should fire (the symlink was skipped first),
	// and certainly no walk/io error.
	if countNotice(res, application.NoticeWalkError) != 0 || countNotice(res, application.NoticeIOError) != 0 {
		t.Errorf("unexpected walk/io error notices: %+v", res.Notices)
	}
	// And we must NOT have ingested the outside file's content as a document.
	for _, f := range res.Files {
		if strings.Contains(f.Path, "secret") {
			t.Errorf("escaping symlink target was scanned: %s", f.Path)
		}
	}
}

func TestScan_SymlinkToInsideRootStillSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "target.md"), "# target")
	link := filepath.Join(root, "alias.md")
	if err := os.Symlink(filepath.Join(root, "target.md"), link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	res := scan(t, root, Config{})
	// No-follow policy: the in-root symlink is still NOT followed; only the real
	// target is discovered.
	if got := ids(res); len(got) != 1 || got[0] != "target.md" {
		t.Errorf("ids = %v, want only [target.md]; symlinks are never followed", got)
	}
	if !hasNotice(res, application.NoticeSkippedSymlink) {
		t.Error("expected a skipped-symlink notice for the in-root link")
	}
}

func TestScan_SymlinkedDirectoryNotTraversed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "inner.md"), "# inner")
	writeFile(t, filepath.Join(root, "real.md"), "# real")
	if err := os.Symlink(outside, filepath.Join(root, "linkeddir")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	res := scan(t, root, Config{})
	for _, f := range res.Files {
		if strings.Contains(f.Path, "inner") {
			t.Errorf("traversed a symlinked directory: %s", f.Path)
		}
	}
	if got := ids(res); len(got) != 1 || got[0] != "real.md" {
		t.Errorf("ids = %v, want only [real.md]", got)
	}
}

func TestScan_NonexistentRootErrors(t *testing.T) {
	_, err := New(Config{}).Scan(context.Background(), filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("scanning a nonexistent root: want error")
	}
}

func TestScan_FileModTimeAndSizePopulated(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# hello")
	res := scan(t, root, Config{})
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(res.Files))
	}
	f := res.Files[0]
	if f.Size == 0 {
		t.Error("Size not populated")
	}
	if f.ModTime.IsZero() {
		t.Error("ModTime not populated")
	}
	if !filepath.IsAbs(f.Path) {
		t.Errorf("Path %q is not absolute", f.Path)
	}
}

func TestScan_Deterministic(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"z.md", "a.md", "m/n.md"} {
		writeFile(t, filepath.Join(root, n), "# x")
	}
	first := ids(scan(t, root, Config{}))
	for i := 0; i < 5; i++ {
		if got := ids(scan(t, root, Config{})); strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("non-deterministic order: %v vs %v", got, first)
		}
	}
}

func TestScan_ExactSizeBoundary(t *testing.T) {
	root := t.TempDir()
	// A file exactly at the cap is kept (the cap is "> cap is too big").
	writeFile(t, filepath.Join(root, "exact.md"), strings.Repeat("x", 100))
	// One byte over is skipped.
	writeFile(t, filepath.Join(root, "over.md"), strings.Repeat("y", 101))

	res := scan(t, root, Config{MaxFileSizeBytes: 100})
	got := ids(res)
	if len(got) != 1 || got[0] != "exact.md" {
		t.Errorf("ids = %v, want [exact.md] (exact-size kept, +1 skipped)", got)
	}
	if !hasNotice(res, application.NoticeOversized) {
		t.Error("expected an oversized notice for the +1 file")
	}
}

func TestScan_UppercaseExtensionsDiscovered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "A.MD"), "# a")
	writeFile(t, filepath.Join(root, "B.Markdown"), "# b")
	writeFile(t, filepath.Join(root, "C.MARKDOWN"), "# c")
	writeFile(t, filepath.Join(root, "skip.TXT"), "x")

	res := scan(t, root, Config{})
	got := ids(res)
	want := []string{"A.MD", "B.Markdown", "C.MARKDOWN"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (extensions matched case-insensitively)", got, want)
	}
}

func TestScan_DoctopusignoreCRLF(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# keep")
	writeFile(t, filepath.Join(root, "secret.md"), "# secret")
	// CRLF line endings (Windows-authored ignore file) must not leave a trailing
	// \r that breaks the pattern.
	writeFile(t, filepath.Join(root, ".doctopusignore"), "secret.md\r\n")

	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want [keep.md] (CRLF .doctopusignore honored)", got)
	}
}

func TestScan_SymlinkedOutputDirExcluded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# keep")

	// Real artifact dir lives elsewhere; a symlink inside the root points to it.
	realOut := t.TempDir()
	writeFile(t, filepath.Join(realOut, "artifact.md"), "# artifact")
	linkOut := filepath.Join(root, "out")
	if err := os.Symlink(realOut, linkOut); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Configure OutputDir as the symlink path; EvalSymlinks should still exclude
	// the real target. (The symlinked dir itself is also not followed.)
	res := scan(t, root, Config{OutputDir: linkOut})
	for _, f := range res.Files {
		if strings.Contains(f.Path, "artifact") {
			t.Errorf("artifact under symlinked output dir was scanned: %s", f.Path)
		}
	}
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want [keep.md]", got)
	}
}
