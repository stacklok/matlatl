package fsscanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
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
	writeFile(t, filepath.Join(root, ".matlatlignore"), "ignored/\ndraft-*.md\n")

	res := scan(t, root, Config{})
	got := ids(res)
	if len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want only [keep.md] (defaults + .matlatlignore honored)", got)
	}
}

// TestScan_DefaultIgnoresPythonCaches asserts the Python virtualenv + tooling
// cache directories (matched by base name anywhere, like node_modules/vendor)
// are skipped wholesale so installed-package / tool-scratch markdown never
// pollutes the corpus, while real docs at peer paths are kept.
func TestScan_DefaultIgnoresPythonCaches(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.md"), "# Real")
	// One markdown file under each newly-added ignore dir, so reverting ANY single
	// name from defaultIgnoredDirs makes this test fail (each name is pinned).
	writeFile(t, filepath.Join(root, ".venv", "pkg", "doc.md"), "# venv")
	writeFile(t, filepath.Join(root, "__pycache__", "x.md"), "# pycache")
	writeFile(t, filepath.Join(root, ".tox", "y.md"), "# tox")
	writeFile(t, filepath.Join(root, "sub", ".mypy_cache", "m.md"), "# mypy")
	writeFile(t, filepath.Join(root, ".pytest_cache", "p.md"), "# pytest")
	writeFile(t, filepath.Join(root, ".ruff_cache", "r.md"), "# ruff")

	got := ids(scan(t, root, Config{}))
	if len(got) != 1 || got[0] != "real.md" {
		t.Errorf("ids = %v, want only [real.md] (python caches skipped anywhere)", got)
	}
}

// TestScan_DefaultIgnoresClaudeWorktrees asserts the scoped default skip for
// `.claude/worktrees` (Claude Code agent worktrees, each a full repo copy) while
// keeping the rest of `.claude` — e.g. `.claude/rules` — in the corpus, since
// docs commonly link into the rules.
func TestScan_DefaultIgnoresClaudeWorktrees(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, ".claude", "rules", "go-style.md"), "# Rules")
	writeFile(t, filepath.Join(root, ".claude", "worktrees", "agent-1", "README.md"), "# Copy")
	writeFile(t, filepath.Join(root, ".claude", "worktrees", "agent-1", "docs", "guide.md"), "# Copy guide")

	got := ids(scan(t, root, Config{}))
	want := []string{".claude/rules/go-style.md", "README.md"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v (worktrees skipped, rules kept)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ids = %v, want %v", got, want)
			break
		}
	}
}

// TestScan_DefaultIgnoresClaudePlans asserts the scoped default skip for
// `.claude/plans` (transient agent scratch) while keeping the deliberately
// non-default `.claude` subtrees in the corpus: `.claude/rules` (real docs),
// `.claude/skills` (real graphs), and `.claude/agents` (judgment call deferred to
// per-repo config, NOT default-ignored).
func TestScan_DefaultIgnoresClaudePlans(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, ".claude", "plans", "scratch.md"), "# Scratch")
	writeFile(t, filepath.Join(root, ".claude", "plans", "deep", "more.md"), "# More")
	writeFile(t, filepath.Join(root, ".claude", "rules", "go-style.md"), "# Rules")
	writeFile(t, filepath.Join(root, ".claude", "skills", "x", "SKILL.md"), "# Skill")
	writeFile(t, filepath.Join(root, ".claude", "agents", "a.md"), "# Agent")

	got := ids(scan(t, root, Config{}))
	want := []string{
		".claude/agents/a.md",
		".claude/rules/go-style.md",
		".claude/skills/x/SKILL.md",
		"README.md",
	}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v (plans skipped; rules/skills/agents kept)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ids = %v, want %v", got, want)
			break
		}
	}
}

// TestScan_DefaultIgnoresClaudeAgentMemory asserts the scoped default skip for
// `.claude/agent-memory` (transient agent-generated memory notes that use a
// non-repo-relative `[[slug]]` wikilink convention which cannot resolve, ADR
// 0018) while keeping the deliberately non-default sibling `.claude` subtrees in
// the corpus: `.claude/rules` (real docs), `.claude/skills` (real graphs), and
// `.claude/agents` (judgment call deferred to per-repo config, NOT
// default-ignored). This pins both the new prune AND that the ignore stays
// narrowly scoped — sibling `.claude` subtrees survive.
func TestScan_DefaultIgnoresClaudeAgentMemory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, ".claude", "agent-memory", "reviewer", "note.md"), "# Note")
	writeFile(t, filepath.Join(root, ".claude", "agent-memory", "deep", "more.md"), "# More")
	writeFile(t, filepath.Join(root, ".claude", "rules", "go-style.md"), "# Rules")
	writeFile(t, filepath.Join(root, ".claude", "skills", "x", "SKILL.md"), "# Skill")
	writeFile(t, filepath.Join(root, ".claude", "agents", "a.md"), "# Agent")

	got := ids(scan(t, root, Config{}))
	want := []string{
		".claude/agents/a.md",
		".claude/rules/go-style.md",
		".claude/skills/x/SKILL.md",
		"README.md",
	}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v (agent-memory skipped; rules/skills/agents kept)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ids = %v, want %v", got, want)
			break
		}
	}
}

// TestScan_NestedRepoGitFilePruned pins ADR 0017 for the SUBMODULE/WORKTREE
// shape: a nested directory whose `.git` is a FILE (a gitfile, `gitdir: …`). The
// entire nested working tree is pruned and exactly one skipped-nested-repo notice
// fires for the dir.
func TestScan_NestedRepoGitFilePruned(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	// A submodule's working tree: a `.git` FILE plus docs beside it.
	writeFile(t, filepath.Join(root, "sub", ".git"), "gitdir: ../.git/modules/sub\n")
	writeFile(t, filepath.Join(root, "sub", "README.md"), "# Submodule")
	writeFile(t, filepath.Join(root, "sub", "docs", "guide.md"), "# Sub guide")

	res := scan(t, root, Config{})
	got := ids(res)
	if len(got) != 1 || got[0] != "README.md" {
		t.Errorf("ids = %v, want only [README.md] (nested submodule working tree pruned)", got)
	}
	if n := countNotice(res, application.NoticeSkippedNestedRepo); n != 1 {
		t.Errorf("skipped-nested-repo notices = %d, want exactly 1", n)
	}
	// The per-directory notice contract: Path is exactly the nested dir (not merely
	// containing "sub", which would also match e.g. "submarine/"). Scan walks from
	// the EvalSymlinks-canonicalized root (ADR 0003), so notice paths are
	// symlink-resolved; resolve the temp dir the same way before comparing (on
	// macOS t.TempDir() is under /var, a symlink to /private/var).
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", root, err)
	}
	if notice, ok := noticeFor(res, application.NoticeSkippedNestedRepo); ok && notice.Path != filepath.Join(realRoot, "sub") {
		t.Errorf("notice path = %q, want exactly %q", notice.Path, filepath.Join(realRoot, "sub"))
	}
}

// TestScan_NestedRepoPrunesOnlyItsSubtree pins that the nested-repo prune uses
// fs.SkipDir (prune just that subtree), NOT fs.SkipAll (abort the whole walk): a
// sibling directory beside the nested repo is still scanned afterward. A
// SkipDir→SkipAll regression would silently drop `other/y.md`.
func TestScan_NestedRepoPrunesOnlyItsSubtree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, "sub", ".git"), "gitdir: ../.git/modules/sub\n")
	writeFile(t, filepath.Join(root, "sub", "x.md"), "# nested")
	writeFile(t, filepath.Join(root, "other", "y.md"), "# sibling")

	res := scan(t, root, Config{})
	got := ids(res)
	want := []string{"README.md", "other/y.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (sibling 'other/' still scanned after nested-repo prune)", got, want)
	}
	if n := countNotice(res, application.NoticeSkippedNestedRepo); n != 1 {
		t.Errorf("skipped-nested-repo notices = %d, want exactly 1", n)
	}
}

// TestScan_TwoNestedReposTwoNotices pins that two distinct nested repos each emit
// their own notice (exactly two), both subtrees are pruned, and the walk
// continues across both (a second SkipDir-not-SkipAll proof).
func TestScan_TwoNestedReposTwoNotices(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, "sub-a", ".git"), "gitdir: ../.git/modules/sub-a\n")
	writeFile(t, filepath.Join(root, "sub-a", "a.md"), "# a")
	writeFile(t, filepath.Join(root, "sub-b", ".git"), "gitdir: ../.git/modules/sub-b\n")
	writeFile(t, filepath.Join(root, "sub-b", "b.md"), "# b")

	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "README.md" {
		t.Errorf("ids = %v, want only [README.md] (both nested subtrees pruned)", got)
	}
	if n := countNotice(res, application.NoticeSkippedNestedRepo); n != 2 {
		t.Errorf("skipped-nested-repo notices = %d, want exactly 2 (one per nested repo)", n)
	}
}

// TestScan_UninitializedSubmoduleNoOp pins the ADR 0017 edge case: an
// uninitialized submodule is an empty placeholder dir with no `.git`. isNestedRepo
// must NOT match it (no over-matching on emptiness/dotfiles) — no notice fires and
// the dir is walked normally (its markdown, if any, is scanned).
func TestScan_UninitializedSubmoduleNoOp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	// A placeholder dir with a non-md file and a regular doc, but no `.git`.
	writeFile(t, filepath.Join(root, "sub", "placeholder.txt"), "not initialized")
	writeFile(t, filepath.Join(root, "sub", "real.md"), "# real doc in non-nested dir")

	res := scan(t, root, Config{})
	got := ids(res)
	want := []string{"README.md", "sub/real.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (uninitialized submodule has no .git; walked normally)", got, want)
	}
	if hasNotice(res, application.NoticeSkippedNestedRepo) {
		t.Error("a dir without a .git marker must not emit a skipped-nested-repo notice")
	}
}

// TestScan_NestedRepoGitDirPruned pins ADR 0017 for the NESTED-CLONE shape: a
// nested directory whose `.git` is a DIRECTORY. Its working-tree doc is pruned and
// one notice fires.
func TestScan_NestedRepoGitDirPruned(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")
	// A nested clone: `.git` is a real directory; a sibling doc is the working tree.
	writeFile(t, filepath.Join(root, "nested", ".git", "config.md"), "# git internals")
	writeFile(t, filepath.Join(root, "nested", "sibling.md"), "# Working tree doc")

	res := scan(t, root, Config{})
	got := ids(res)
	if len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want only [keep.md] (nested clone working tree pruned)", got)
	}
	if n := countNotice(res, application.NoticeSkippedNestedRepo); n != 1 {
		t.Errorf("skipped-nested-repo notices = %d, want exactly 1", n)
	}
}

// TestScan_RootGitDirExemptFromNestedRepoPrune is the critical root-exemption
// case: the scan root itself has a `.git` (here a directory) yet its own docs are
// still discovered and NO nested-repo notice fires for the root.
func TestScan_RootGitDirExemptFromNestedRepoPrune(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "config.md"), "# git internals")
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, "docs", "g.md"), "# Guide")

	res := scan(t, root, Config{})
	got := ids(res)
	want := []string{"README.md", "docs/g.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (root's own .git is exempt; .git dir pruned by name)", got, want)
	}
	if hasNotice(res, application.NoticeSkippedNestedRepo) {
		t.Error("the scan root's own .git must not emit a skipped-nested-repo notice")
	}
}

// TestScan_RootGitFileExemptStillScans pins the escape hatch: running matlatl
// directly ON a directory that contains a `.git` FILE (i.e. scanning a submodule
// as the root) still scans its markdown — the root is exempt from the prune.
func TestScan_RootGitFileExemptStillScans(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git"), "gitdir: /elsewhere/.git/modules/this\n")
	writeFile(t, filepath.Join(root, "README.md"), "# Submodule scanned directly")

	res := scan(t, root, Config{})
	got := ids(res)
	if len(got) != 1 || got[0] != "README.md" {
		t.Errorf("ids = %v, want [README.md] (root with .git FILE is exempt, still scanned)", got)
	}
	if hasNotice(res, application.NoticeSkippedNestedRepo) {
		t.Error("scanning a submodule directly must not emit a skipped-nested-repo notice")
	}
}

// TestScan_NestedRepoOneNoticePerRepo asserts a nested repo with many markdown
// files yields exactly ONE notice (the SkipDir prunes the subtree before any
// content is visited).
func TestScan_NestedRepoOneNoticePerRepo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Root")
	writeFile(t, filepath.Join(root, "sub", ".git"), "gitdir: ../.git/modules/sub\n")
	for _, n := range []string{"a.md", "b.md", "c.md", "deep/d.md", "deep/e.md"} {
		writeFile(t, filepath.Join(root, "sub", n), "# "+n)
	}

	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "README.md" {
		t.Errorf("ids = %v, want only [README.md]", got)
	}
	if n := countNotice(res, application.NoticeSkippedNestedRepo); n != 1 {
		t.Errorf("skipped-nested-repo notices = %d, want exactly 1 per nested repo", n)
	}
}

// TestScan_ExplicitIgnoreOnNestedRepoIsSilent pins that an explicit
// .matlatlignore match on the nested dir wins AND stays silent: the dir is pruned
// with NO skipped-nested-repo notice (the check runs after shouldSkipDir).
func TestScan_ExplicitIgnoreOnNestedRepoIsSilent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")
	writeFile(t, filepath.Join(root, "sub", ".git"), "gitdir: ../.git/modules/sub\n")
	writeFile(t, filepath.Join(root, "sub", "README.md"), "# Submodule")
	writeFile(t, filepath.Join(root, ".matlatlignore"), "sub/\n")

	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want only [keep.md]", got)
	}
	if hasNotice(res, application.NoticeSkippedNestedRepo) {
		t.Error("an explicitly ignored nested repo must be silent (explicit ignore wins)")
	}
}

// TestScan_NestedRepoDeterministic asserts repeated scans over a tree with a
// nested repo produce identical ids.
func TestScan_NestedRepoDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "z.md"), "# z")
	writeFile(t, filepath.Join(root, "a.md"), "# a")
	writeFile(t, filepath.Join(root, "sub", ".git"), "gitdir: ../.git/modules/sub\n")
	writeFile(t, filepath.Join(root, "sub", "ignored.md"), "# nested")

	first := ids(scan(t, root, Config{}))
	for i := 0; i < 5; i++ {
		if got := ids(scan(t, root, Config{})); strings.Join(got, ",") != strings.Join(first, ",") {
			t.Fatalf("non-deterministic order: %v vs %v", got, first)
		}
	}
}

// TestScan_NestedRepoGitSymlinkPruned pins that a `.git` SYMLINK is detected by
// presence (Lstat, not followed) and the nested tree is still pruned (ADR 0003 +
// 0017).
func TestScan_NestedRepoGitSymlinkPruned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")
	writeFile(t, filepath.Join(root, "sub", "README.md"), "# Submodule")
	// `.git` as a symlink (target need not exist/resolve): Lstat sees its presence.
	if err := os.Symlink("/elsewhere/gitdir", filepath.Join(root, "sub", ".git")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want only [keep.md] (nested repo with symlinked .git pruned)", got)
	}
	if n := countNotice(res, application.NoticeSkippedNestedRepo); n != 1 {
		t.Errorf("skipped-nested-repo notices = %d, want exactly 1", n)
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

// TestScan_IgnoredSymlinkProducesNoNotice pins #8: an explicitly ignored symlink
// must be fully silent — no skipped-symlink notice and no corpus entry — because
// the .matlatlignore match now runs before the symlink notice. (A NON-ignored
// symlink still gets the notice; see TestScan_SymlinkToInsideRootStillSkipped.)
func TestScan_IgnoredSymlinkProducesNoNotice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "target.md"), "# target")
	if err := os.Symlink(filepath.Join(root, "target.md"), filepath.Join(root, "alias.md")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	writeFile(t, filepath.Join(root, ".matlatlignore"), "alias.md\n")

	res := scan(t, root, Config{})
	// The ignored symlink is silenced: no skipped-symlink notice fires for it.
	if hasNotice(res, application.NoticeSkippedSymlink) {
		t.Error("an ignored symlink must not emit a skipped-symlink notice (#8)")
	}
	// The no-follow policy is unchanged: only the real target is in the corpus.
	if got := ids(res); len(got) != 1 || got[0] != "target.md" {
		t.Errorf("ids = %v, want only [target.md]; the symlink is never followed", got)
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
	// ADR 0003 "reported" half: the skipped directory symlink is noticed.
	if !hasNotice(res, application.NoticeSkippedSymlink) {
		t.Error("expected a skipped-symlink notice for the directory symlink")
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

func TestScan_MatlatlignoreCRLF(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.md"), "# keep")
	writeFile(t, filepath.Join(root, "secret.md"), "# secret")
	// CRLF line endings (Windows-authored ignore file) must not leave a trailing
	// \r that breaks the pattern.
	writeFile(t, filepath.Join(root, ".matlatlignore"), "secret.md\r\n")

	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want [keep.md] (CRLF .matlatlignore honored)", got)
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

// TestScan_OversizedIgnoreFileSkippedGracefully is the ADR-0003 invariant-3
// regression test for the ignore-file OOM vector: .matlatlignore is read before
// the WalkDir loop and therefore is NOT covered by MaxFileSizeBytes. A hostile,
// multi-GB ignore file must be skipped (os.Stat-gated at maxIgnoreBytes) without
// reading it into memory, and the scan must still complete and discover the
// markdown. We write an ignore file just over the cap; the scan must succeed,
// ignore no rules from the oversized file, and find keep.md.
func TestScan_OversizedIgnoreFileSkippedGracefully(t *testing.T) {
	root := t.TempDir()
	// An oversized ignore file: > maxIgnoreBytes. If the rule "keep.md" inside it
	// were honored, keep.md would be excluded — so finding keep.md proves the
	// oversized file was skipped entirely, not parsed.
	big := make([]byte, maxIgnoreBytes+4096)
	for i := range big {
		big[i] = '\n'
	}
	copy(big, []byte("keep.md\n"))
	if err := os.WriteFile(filepath.Join(root, ignoreFileName), big, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")

	// Must not error or OOM, and must discover the markdown.
	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want [keep.md] (oversized .matlatlignore skipped, not applied)", got)
	}
}

// TestScan_SymlinkedIgnoreFileNotFollowed pins the no-symlink-escape invariant
// (ADR 0003 invariant 1) for the pre-walk .matlatlignore read: a .matlatlignore
// that is a SYMLINK to a file OUTSIDE the scan root must NOT be followed. The
// loader Lstat-skips it, so its rules never apply — proven by keep.md still being
// discovered even though the symlink target would have excluded it.
func TestScan_SymlinkedIgnoreFileNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	// A rules file OUTSIDE the scan root that, if followed, WOULD drop keep.md.
	outside := t.TempDir()
	target := filepath.Join(outside, "evil-ignore")
	if err := os.WriteFile(target, []byte("keep.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, ignoreFileName)); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")

	// The symlinked ignore file is not followed, so keep.md is still discovered.
	res := scan(t, root, Config{})
	if got := ids(res); len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want [keep.md] (symlinked .matlatlignore not followed)", got)
	}
}

// TestScan_IgnoreNegationReincludes pins the current behavior of go-gitignore's
// '!' negation (re-inclusion): a later '!' pattern re-includes a path excluded
// by an earlier pattern. (The dependency's source carries a stale TODO claiming
// negation is unimplemented; it IS implemented in MatchesPathHow. This test
// guards the behavior so a future dep swap cannot silently change it.)
func TestScan_IgnoreNegationReincludes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".matlatlignore"), "*.md\n!keep.md\n")
	writeFile(t, filepath.Join(root, "keep.md"), "# Keep")
	writeFile(t, filepath.Join(root, "drop.md"), "# Drop")

	res := scan(t, root, Config{})
	got := ids(res)
	if len(got) != 1 || got[0] != "keep.md" {
		t.Errorf("ids = %v, want [keep.md] (negation '!keep.md' re-includes it; '*.md' drops drop.md)", got)
	}
}
