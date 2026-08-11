package fsscanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/platform"
)

// The tests in this file cover ADR 0024 (--respect-gitignore): the opt-in union
// of the repo's effective git-ignore set with .matlatlignore. They shell out to
// the real git binary (like the feature does) and skip when it is absent.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
}

// gitInit creates a git work tree at root with the given committed files, then
// writes (but does NOT commit) the given untracked files — the shape that
// motivates ADR 0024: local-only, gitignored working files on disk beside a
// clean committed tree.
func gitInit(t *testing.T, root string, committed, untracked map[string]string) {
	t.Helper()
	requireGit(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	for name, content := range committed {
		writeFile(t, filepath.Join(root, name), content)
	}
	run("add", "-A")
	run("commit", "-qm", "init")
	for name, content := range untracked {
		writeFile(t, filepath.Join(root, name), content)
	}
}

func TestScan_RespectGitignoreExcludesIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root,
		map[string]string{
			"README.md":     "# Root",
			".gitignore":    "HANDOFF.md\n*.local.md\nscratch/\n",
			"docs/guide.md": "# Guide",
		},
		map[string]string{
			"HANDOFF.md":       "# handoff",
			"CLAUDE.local.md":  "# local",
			"scratch/notes.md": "# scratch",
		},
	)

	// Off by default: the gitignored working files are in the corpus.
	res := scan(t, root, Config{})
	want := []string{"CLAUDE.local.md", "HANDOFF.md", "README.md", "docs/guide.md", "scratch/notes.md"}
	if got := ids(res); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("default ids = %v, want %v (gitignore NOT respected by default)", got, want)
	}

	// On: only the committed tree remains; the ignored files are silently gone
	// (no notices — an ignored path is fully silent, like .matlatlignore).
	res = scan(t, root, Config{RespectGitignore: true})
	want = []string{"README.md", "docs/guide.md"}
	if got := ids(res); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("respect-gitignore ids = %v, want %v", got, want)
	}
	if len(res.Notices) != 0 {
		t.Errorf("notices = %v, want none (git-ignored paths are silent)", res.Notices)
	}
}

// TestScan_RespectGitignoreNestedNegation pins that the corpus matches GIT's
// OWN verdicts — the reason the feature derives its set from git rather than
// re-implementing gitignore precedence. Two shapes:
//
//  1. A parent dir-ignore (`logs/`) can NOT be re-included by a nested
//     .gitignore '!' — git prunes the excluded directory before ever reading
//     logs/.gitignore (check-ignore confirms logs/keep.md is ignored), so
//     matlatl must exclude it too.
//  2. A file-level parent ignore (`build/*`) CAN be re-included by a nested
//     '!' — git descends, honors build/!keep.md, and matlatl must keep it.
func TestScan_RespectGitignoreNestedNegation(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root,
		map[string]string{
			"README.md":        "# Root",
			".gitignore":       "logs/\nbuild/*\n",
			"logs/.gitignore":  "!keep.md\n",
			"logs/keep.md":     "# inside an ignored dir: still ignored by git",
			"build/.gitignore": "!keep.md\n",
			"build/keep.md":    "# re-included by nested negation",
			"docs/guide.md":    "# Guide",
		},
		map[string]string{
			"logs/drop.md":  "# dropped",
			"build/drop.md": "# dropped",
		},
	)

	res := scan(t, root, Config{RespectGitignore: true})
	want := []string{"README.md", "build/keep.md", "docs/guide.md"}
	if got := ids(res); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (git's exact nested-negation semantics)", got, want)
	}
}

// TestScan_RespectGitignoreMatlatlignoreFinalWord pins the union's precedence
// (ADR 0024): the committed .matlatlignore is the final word — its '!' can
// re-include a path git ignores (reproducing the file in CI), and its plain
// rules still exclude on top of the git set.
func TestScan_RespectGitignoreMatlatlignoreFinalWord(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root,
		map[string]string{
			"README.md":  "# Root",
			".gitignore": "HANDOFF.md\n",
		},
		map[string]string{
			"HANDOFF.md":   "# handoff",
			"extra.md":     "# extra",
			"scratch/a.md": "# scratch",
		},
	)
	writeFile(t, filepath.Join(root, ".matlatlignore"), "!HANDOFF.md\nscratch/\n")

	res := scan(t, root, Config{RespectGitignore: true})
	want := []string{"HANDOFF.md", "README.md", "extra.md"}
	if got := ids(res); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (.matlatlignore '!' re-includes a git-ignored file; .matlatlignore dir rule still excludes)", got, want)
	}
}

// TestScan_RespectGitignoreNonGitRootNotices pins the documented no-op: on a
// root that is not a git work tree the feature fail-opens — the scan proceeds
// with .matlatlignore only and exactly one gitignore notice explains why.
func TestScan_RespectGitignoreNonGitRootNotices(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# a")

	res := scan(t, root, Config{RespectGitignore: true})
	if got := ids(res); len(got) != 1 || got[0] != "a.md" {
		t.Errorf("ids = %v, want [a.md] (fail-open: scan proceeds)", got)
	}
	if n := countNotice(res, application.NoticeGitignore); n != 1 {
		t.Errorf("gitignore notices = %d, want exactly 1", n)
	}
}

// TestScan_RespectGitignoreEmptyIgnoreSet pins that a repo with no ignored
// files collects an empty set and scans everything (no spurious exclusions, no
// notice).
func TestScan_RespectGitignoreEmptyIgnoreSet(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root,
		map[string]string{"README.md": "# Root"},
		map[string]string{"draft.md": "# untracked but NOT ignored"},
	)

	res := scan(t, root, Config{RespectGitignore: true})
	want := []string{"README.md", "draft.md"}
	if got := ids(res); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (untracked-but-not-ignored files stay)", got, want)
	}
	if hasNotice(res, application.NoticeGitignore) {
		t.Errorf("unexpected gitignore notice: %v", res.Notices)
	}
}

// TestScan_RespectGitignoreDeterministic pins byte-stable behavior across
// repeated scans (the corpus and notices must not depend on git output
// ordering beyond ls-files' stable index order).
func TestScan_RespectGitignoreDeterministic(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root,
		map[string]string{
			"README.md":  "# Root",
			".gitignore": "a.md\nb.md\nc/\n",
		},
		map[string]string{
			"a.md":   "# a",
			"b.md":   "# b",
			"c/x.md": "# x",
		},
	)

	first := scan(t, root, Config{RespectGitignore: true})
	for i := 0; i < 3; i++ {
		res := scan(t, root, Config{RespectGitignore: true})
		if strings.Join(ids(res), ",") != strings.Join(ids(first), ",") {
			t.Fatalf("run %d ids = %v, first = %v", i, ids(res), ids(first))
		}
		if len(res.Notices) != len(first.Notices) {
			t.Fatalf("run %d notices = %v, first = %v", i, res.Notices, first.Notices)
		}
	}
}

// TestScan_RespectGitignoreDoesNotFollowGitfile verifies the collection is
// rooted at the scan root even when git would resolve a worktree gitfile: the
// corpus is still derived from the realRoot walk (the git subprocess only
// supplies the ignore SET). This pins that scanning a linked worktree root
// excludes what git ignores THERE.
func TestScan_RespectGitignoreWeirdFilenames(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root,
		map[string]string{
			"README.md":  "# Root",
			".gitignore": "weird name.md\n",
		},
		map[string]string{
			"weird name.md": "# space in name",
		},
	)

	res := scan(t, root, Config{RespectGitignore: true})
	if got := ids(res); len(got) != 1 || got[0] != "README.md" {
		t.Errorf("ids = %v, want [README.md] (space-containing ignored path parsed via -z)", got)
	}
}

// TestGitIgnoreLines pins the path→line conversion: an ignored directory
// (trailing slash from --directory) yields both the bare name and the
// trailing-slash subtree form so the walk prunes it at the directory entry.
func TestGitIgnoreLines(t *testing.T) {
	got := gitIgnoreLines([]string{"a.md", "dir/", "deep/nested/"})
	want := []string{"a.md", "dir", "dir/", "deep/nested", "deep/nested/"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("gitIgnoreLines = %v, want %v", got, want)
	}
}

// TestCollectGitIgnored_NoGitBinary pins the fail-open notice when git is
// absent, simulated with an empty PATH.
func TestCollectGitIgnored_NoGitBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	paths, notice := collectGitIgnored(t.Context(), t.TempDir())
	if paths != nil {
		t.Errorf("paths = %v, want nil on missing git", paths)
	}
	if notice == nil || notice.Kind != application.NoticeGitignore {
		t.Errorf("notice = %v, want one gitignore notice", notice)
	}
}

// TestScan_RespectGitignoreOversizedMatlatlignoreStillAppliesGitSet pins that
// an oversized .matlatlignore (skipped per the ADR 0003 cap) does not take the
// git-derived set down with it.
func TestScan_RespectGitignoreOversizedMatlatlignoreStillAppliesGitSet(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root,
		map[string]string{
			"README.md":  "# Root",
			".gitignore": "HANDOFF.md\n",
		},
		map[string]string{
			"HANDOFF.md": "# handoff",
		},
	)
	// Over the 1 MiB pre-walk read cap: skipped, not read.
	big := make([]byte, platform.PreWalkReadCap+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(root, ".matlatlignore"), big, 0o644); err != nil {
		t.Fatal(err)
	}

	res := scan(t, root, Config{RespectGitignore: true})
	if got := ids(res); len(got) != 1 || got[0] != "README.md" {
		t.Errorf("ids = %v, want [README.md] (git set applies even when .matlatlignore is skipped)", got)
	}
}
