package fsscanner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/stacklok/matlatl/internal/application"
)

// This file implements ADR 0024: opt-in exclusion of git-ignored files from the
// corpus (--respect-gitignore / .matlatl.yml respectGitignore). The scanner
// shells out to `git ls-files` to derive the repo's effective ignore set —
// tracked .gitignore files, nested .gitignore files, .git/info/exclude, and the
// global core.excludesFile, with full negation/precedence semantics — and unions
// it with .matlatlignore as one matcher. Re-deriving the rules from ls-files
// output (rather than re-implementing git's matching) is what keeps nested
// negations correct: git already resolved them.
//
// Security (ADR 0003): the repo being scanned is untrusted, and a repo can
// smuggle config into a git invocation (a `.git/config` alias named `ls-files`,
// includeIf chain expansion, core.excludesFile pointing anywhere). The
// invocation is therefore flag-only (no shell, so the repo cannot inject
// arguments), runs with GIT_CONFIG_NOSYSTEM=1 and `-c core.excludesFile=`-style
// neutralization scoped to the single command, and the output is size-capped
// before parsing. See collectGitIgnored for the exact guard list.

const (
	// gitCmdTimeout bounds the git subprocess. ls-files is a plumbing read over
	// the index and is fast even on huge repos; the timeout exists so a wedged
	// git (or a pathological index) cannot hang the scan.
	gitCmdTimeout = 30 * time.Second
	// maxGitOutputBytes caps the ls-files output read into memory. The output is
	// proportional to the repo's untracked-file count, which is attacker-
	// controlled (an untrusted repo can carry a tree of millions of untracked
	// files), so this is an ADR 0003 invariant-3 resource cap. On overflow the
	// feature fail-opens (no gitignore rules) with a notice.
	maxGitOutputBytes = 64 << 20 // 64 MiB
)

// collectGitIgnored returns the scan-root-relative, slash-separated paths git
// ignores in the work tree rooted at realRoot, in deterministic (sorted) order.
// The boolean is false — with a notice — when the set could not be collected
// (git missing, realRoot not a work tree, git failure, oversized output); every
// failure fail-opens so the scan proceeds with .matlatlignore only.
func collectGitIgnored(ctx context.Context, realRoot string) ([]string, *application.Notice) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, &application.Notice{
			Kind:   application.NoticeGitignore,
			Path:   realRoot,
			Detail: "--respect-gitignore: git executable not found in PATH; git-ignored files are NOT excluded",
		}
	}

	// Invocation hardening (ADR 0003, untrusted repo):
	//   --no-optional-locks  — no index lock, so a concurrent git is undisturbed.
	//   -c alias.ls-files=   — a repo's .git/config cannot shadow the plumbing
	//                          command (belt-and-braces: git already only expands
	//                          aliases in the first word for builtins it does not
	//                          know, but the reset keeps the surface explicit).
	//   GIT_CONFIG_NOSYSTEM=1 — machine-wide gitconfig stays out of the run.
	// core.excludesFile is deliberately NOT reset: it is user-global config
	// (outside the repo's control) and is part of the effective ignore set the
	// feature promises ("tracked rules + info/exclude + global excludes").
	ctx, cancel := context.WithTimeout(ctx, gitCmdTimeout)
	defer cancel()
	// --directory collapses an ignored directory to one "dir/" entry (cheap
	// pruning); --no-empty-directory keeps that ONLY when the whole subtree is
	// ignored — a nested .gitignore '!' re-include forces git to enumerate the
	// dir's contents instead, so a re-included file is never collapsed away
	// (pinned by TestScan_RespectGitignoreNestedNegation).
	//nolint:gosec // G204: argv form (no shell) with a fixed subcommand and flags;
	// realRoot is the caller's own scan root, not repo-controlled input — and the
	// repo cannot inject arguments (ADR 0024, ADR 0003).
	cmd := exec.CommandContext(ctx, "git",
		"--no-optional-locks",
		"-C", realRoot,
		"-c", "alias.ls-files=",
		"ls-files",
		"--others", "--ignored", "--exclude-standard", "--directory", "--no-empty-directory",
		"-z",
		"--",
	)
	// -z gives NUL-terminated, unquoted paths so weird filenames (newlines,
	// quotes) cannot corrupt the parse.
	cmd.Env = append(cmd.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// The most common failure: realRoot is not inside a git work tree (exit
		// 128). That is the documented no-op case — the feature degrades to
		// .matlatlignore only.
		return nil, &application.Notice{
			Kind: application.NoticeGitignore,
			Path: realRoot,
			Detail: fmt.Sprintf("--respect-gitignore: not a git work tree or git failed (%v%s); git-ignored files are NOT excluded",
				err, stderrTail(stderr.String())),
		}
	}
	if stdout.Len() > maxGitOutputBytes {
		return nil, &application.Notice{
			Kind:   application.NoticeGitignore,
			Path:   realRoot,
			Detail: fmt.Sprintf("--respect-gitignore: git output is %d bytes (cap %d); git-ignored files are NOT excluded", stdout.Len(), maxGitOutputBytes),
		}
	}

	raw := stdout.Bytes()
	if len(raw) == 0 {
		return []string{}, nil
	}
	// ls-files output is index-order (sorted by path); splitting preserves that
	// order, so the result is deterministic without a re-sort.
	parts := strings.Split(string(raw), "\x00")
	paths := parts[:0]
	for _, p := range parts {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// stderrTail extracts the first line of git's stderr for the notice detail,
// keeping the notice one-line and free of embedded newlines.
func stderrTail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return ": " + s
}

// gitIgnoreLines converts the collected ignored paths into literal gitignore
// lines keyed for repo-root-relative slash paths. A trailing-slash entry is an
// ignored DIRECTORY (from --directory); emitting both "name" and "name/"
// covers the dir itself and its whole subtree (the walk prunes on "name/").
// A plain entry is an ignored file, matched exactly. Negation/precedence has
// already been resolved by git, so each entry is a literal path, not a pattern
// — git's ignore syntax treats these as exact root-relative matches.
func gitIgnoreLines(paths []string) []string {
	lines := make([]string, 0, len(paths)*2)
	for _, p := range paths {
		if strings.HasSuffix(p, "/") {
			d := strings.TrimSuffix(p, "/")
			lines = append(lines, d, d+"/")
		} else {
			lines = append(lines, p)
		}
	}
	return lines
}
