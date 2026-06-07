// Package fsscanner implements application.FileScanner: it walks a repository
// root, discovers markdown files, applies ignore rules, and enforces the
// security boundary of ADR 0003 (no symlink following, root containment,
// resource caps). It returns a deterministic, sorted file list plus notices for
// anything skipped.
package fsscanner

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/platform"
)

// Default resource caps (ADR 0003). They are safe-by-default and overridable
// via Config.
const (
	// DefaultMaxFileSizeBytes is the per-file size cap; larger files are skipped.
	DefaultMaxFileSizeBytes int64 = 10 << 20 // 10 MiB
	// DefaultMaxFiles is the discovery cap; discovery stops once it is reached.
	DefaultMaxFiles = 10_000
	// ignoreFileName is the per-root ignore file (gitignore semantics).
	ignoreFileName = ".matlatlignore"
	// maxIgnoreBytes caps the size of .matlatlignore that we will read into
	// memory. The ignore file is read BEFORE the WalkDir loop (and therefore
	// before the per-file MaxFileSizeBytes guard applies), so a hostile repo
	// could otherwise hand us a multi-GB ignore file and OOM the scan (ADR 0003
	// invariant 3). It is the shared pre-walk read cap (platform.PreWalkReadCap,
	// the single audit point for this limit, shared with the config loader's
	// .matlatl.yml read): far more than any real ignore file needs; an oversized
	// ignore file is skipped (treated like a missing file).
	maxIgnoreBytes = platform.PreWalkReadCap
)

// defaultIgnoredDirs are directory base names skipped wholesale during the walk,
// matched anywhere in the tree. Alongside the VCS/dependency caches (.git,
// node_modules, vendor) we skip the common Python virtualenv + tooling caches
// (.venv, .tox, __pycache__, .mypy_cache, .pytest_cache, .ruff_cache): these
// hold installed packages and tool scratch state whose markdown is never the
// repo's own documentation.
//
// Build-output directories (dist, build, target, site, out) and editor dirs are
// DELIBERATELY NOT listed here: those can legitimately contain generated docs a
// repo wants scanned (e.g. a site/ of rendered markdown), so suppressing them
// belongs in a per-repo .matlatlignore, not a global default. The line is drawn
// at "package/tool caches that are never authored docs"; anything that might be
// real documentation stays in scope by default.
var defaultIgnoredDirs = []string{
	".git",
	".mypy_cache",
	".pytest_cache",
	".ruff_cache",
	".tox",
	".venv",
	"__pycache__",
	"node_modules",
	"vendor",
}

// defaultIgnoredRelPaths are scan-root-relative directory paths skipped wholesale
// during the walk. Unlike defaultIgnoredDirs (matched by base name anywhere),
// these match one specific location so the skip stays scoped. `.claude/worktrees`
// holds Claude Code agent worktrees — each a FULL copy of the repository — which
// would otherwise multiply the corpus many times over; `.claude/plans` holds
// transient scratch plans (ADR 0003). The line is drawn deliberately narrow:
// other `.claude/*` subtrees (agents, agent-memory, skills, rules) are NOT
// default-ignored — those are judgment calls deferred to per-repo config or are
// real docs/graphs handled by the roots mechanism. Slash-separated to match
// relForMatch output.
var defaultIgnoredRelPaths = []string{".claude/plans", ".claude/worktrees"}

// Config tunes a Scanner. The zero value is not valid; use New, which fills in
// safe defaults for any unset field.
type Config struct {
	// MaxFileSizeBytes is the per-file size cap; files above it are skipped and
	// noticed, never read.
	MaxFileSizeBytes int64
	// MaxFiles caps discovery; once reached, discovery stops and a truncation
	// notice is emitted.
	MaxFiles int
	// OutputDir, when non-empty, is excluded from the walk (so a re-scan does
	// not ingest its own artifacts).
	OutputDir string
}

// Scanner discovers markdown files under a root.
type Scanner struct {
	cfg Config
}

// New returns a Scanner, filling unset Config fields with safe defaults.
func New(cfg Config) *Scanner {
	if cfg.MaxFileSizeBytes <= 0 {
		cfg.MaxFileSizeBytes = DefaultMaxFileSizeBytes
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = DefaultMaxFiles
	}
	return &Scanner{cfg: cfg}
}

// compile-time assertion that Scanner implements the port.
var _ application.FileScanner = (*Scanner)(nil)

// Scan walks root and returns the markdown files to parse plus notices. The
// returned file list is sorted by DocumentID for determinism. It honors context
// cancellation.
func (s *Scanner) Scan(ctx context.Context, root string) (application.ScanResult, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return application.ScanResult{}, fmt.Errorf("fsscanner: resolve root %q: %w", root, err)
	}
	// Canonicalize the root (resolving any symlinks in the root path itself) so
	// containment checks compare real paths (ADR 0003).
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return application.ScanResult{}, fmt.Errorf("fsscanner: canonicalize root %q: %w", root, err)
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return application.ScanResult{}, fmt.Errorf("fsscanner: stat root %q: %w", root, err)
	}
	if !info.IsDir() {
		return application.ScanResult{}, fmt.Errorf("fsscanner: root %q is not a directory", root)
	}

	matcher := s.loadIgnore(realRoot)

	var (
		files    []application.ScannedFile
		notices  []application.Notice
		absOut   string
		exceeded bool
	)
	if s.cfg.OutputDir != "" {
		if abs, aerr := filepath.Abs(s.cfg.OutputDir); aerr == nil {
			// Canonicalize so a symlinked output dir is still recognized and
			// excluded. EvalSymlinks fails if the dir does not exist yet; fall
			// back to the absolute path in that case.
			if real, eerr := filepath.EvalSymlinks(abs); eerr == nil {
				absOut = real
			} else {
				absOut = abs
			}
		}
	}

	walkErr := filepath.WalkDir(realRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable entry: record a notice and keep going rather than aborting.
			notices = append(notices, application.Notice{
				Kind:   application.NoticeWalkError,
				Path:   path,
				Detail: fmt.Sprintf("walk error: %v", err),
			})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}

		// Directory handling: prune ignored / output dirs.
		if d.IsDir() {
			if path == realRoot {
				return nil
			}
			if s.shouldSkipDir(realRoot, path, d.Name(), absOut, matcher) {
				return fs.SkipDir
			}
			return nil
		}

		// Respect ignore rules FIRST, for every entry (#8): an explicitly ignored
		// path — including a symlink — must produce no notice and no corpus entry.
		// This runs before the symlink and markdown checks so an ignored symlink is
		// fully silent. The no-follow policy (ADR 0003) is unchanged: an ignored
		// symlink is skipped regardless; only its notice is suppressed. (relForMatch
		// is a path-relativization for matching — it does not follow the symlink.)
		if rel, ok := relForMatch(realRoot, path); ok && matcher != nil && matcher.MatchesPath(rel) {
			return nil
		}

		// No symlink following (ADR 0003): a non-ignored symlinked entry is skipped
		// and noticed.
		if d.Type()&fs.ModeSymlink != 0 {
			notices = append(notices, s.symlinkNotice(realRoot, path))
			return nil
		}

		if !identity.IsMarkdownPath(d.Name()) {
			return nil
		}

		// Containment: the real path must stay under the real root. Guards
		// against any residual escape (e.g. a parent component being a symlink).
		realPath, rerr := filepath.EvalSymlinks(path)
		if rerr != nil || !underRoot(realRoot, realPath) {
			notices = append(notices, application.Notice{
				Kind:   application.NoticeEscapesRoot,
				Path:   path,
				Detail: "path resolves outside the scan root; skipped",
			})
			return nil
		}

		// Stat the canonical (real) path, not the walk path, and store realPath in
		// ScannedFile.Path so the parser reads exactly what we validated. This
		// NARROWS the EvalSymlinks→read TOCTOU window but does not close it: a
		// residual Lstat→ReadFile race remains (an attacker who can swap the path
		// between our check and the parser's open). Truly closing it needs
		// openat/O_NOFOLLOW-style handle-based I/O (no portable stdlib API today);
		// the residual window is accepted for a batch scanner over a local tree.
		fi, ierr := os.Lstat(realPath)
		if ierr != nil {
			notices = append(notices, application.Notice{
				Kind:   application.NoticeIOError,
				Path:   path,
				Detail: fmt.Sprintf("stat failed: %v", ierr),
			})
			return nil
		}
		if fi.Size() > s.cfg.MaxFileSizeBytes {
			notices = append(notices, application.Notice{
				Kind:   application.NoticeOversized,
				Path:   path,
				Detail: fmt.Sprintf("file is %d bytes (cap %d); skipped", fi.Size(), s.cfg.MaxFileSizeBytes),
			})
			return nil
		}

		id, derr := identity.NewDocumentID(realRoot, realPath)
		if derr != nil {
			notices = append(notices, application.Notice{
				Kind:   application.NoticeIOError,
				Path:   path,
				Detail: fmt.Sprintf("cannot derive document id: %v", derr),
			})
			return nil
		}

		if len(files) >= s.cfg.MaxFiles {
			exceeded = true
			return errStopWalk
		}

		files = append(files, application.ScannedFile{
			Path:    realPath,
			ID:      id,
			ModTime: fi.ModTime(),
			Size:    fi.Size(),
		})
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, errStopWalk) {
		return application.ScanResult{}, fmt.Errorf("fsscanner: walk %q: %w", root, walkErr)
	}
	if exceeded {
		notices = append(notices, application.Notice{
			Kind:   application.NoticeTruncated,
			Path:   realRoot,
			Detail: fmt.Sprintf("discovery stopped at MaxFiles=%d; additional files were not scanned", s.cfg.MaxFiles),
		})
	}

	slices.SortFunc(files, func(a, b application.ScannedFile) int {
		return cmp.Compare(a.ID, b.ID)
	})

	return application.ScanResult{Files: files, Notices: notices}, nil
}

// errStopWalk is a sentinel used to halt WalkDir at the file-count cap.
var errStopWalk = errors.New("fsscanner: max files reached")

// loadIgnore compiles root/.matlatlignore if present, returning nil when
// absent, unreadable, or oversized (none of these are an error — a repo without
// a usable ignore file simply has no ignore rules).
//
// Security (ADR 0003): the ignore file is read BEFORE the WalkDir loop, so
// neither the per-file MaxFileSizeBytes guard (invariant 3) nor the walk's
// no-symlink-follow stance (invariant 1) covers it automatically. We therefore
// Lstat it first: a symlink is NOT followed (skipped, like the walk skips
// symlinks), and a file larger than maxIgnoreBytes is skipped rather than read,
// so a hostile repo cannot OOM the scan with a multi-GB .matlatlignore nor
// escape the root via a symlinked one. We then read the capped bytes ourselves
// and hand them to CompileIgnoreLines rather than CompileIgnoreFile (whose
// internal ReadFile has no size cap). Splitting on "\n" and trimming any
// trailing "\r" preserves CompileIgnoreFile's CRLF handling. A skipped symlink
// is silent here, matching loadIgnore's existing silent-skip-on-missing posture
// (it returns no notice channel).
//
// Note: go-gitignore supports gitignore '!' negation (re-inclusion) patterns;
// behavior is pinned by TestLoadIgnore_NegationReincludes. (A historical TODO in
// the dependency's source claims negation is unimplemented; it is implemented in
// MatchesPathHow as of the pinned version, and the test guards against a
// regression if the dep is ever swapped.)
func (s *Scanner) loadIgnore(realRoot string) *ignore.GitIgnore {
	p := filepath.Join(realRoot, ignoreFileName)
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		// missing / symlink / not a regular file: no ignore rules. Lstat (not
		// Stat) means a symlink reports its own mode (ModeSymlink, not regular),
		// so it falls into this branch and is not followed (ADR 0003 invariant 1).
		return nil
	}
	if fi.Size() > maxIgnoreBytes {
		return nil // oversized: skip gracefully rather than read it into memory.
	}
	b, err := os.ReadFile(p) //nolint:gosec // size-capped above (ADR 0003 invariant 3)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSuffix(ln, "\r")
	}
	return ignore.CompileIgnoreLines(lines...)
}

// shouldSkipDir reports whether a directory should be pruned from the walk.
func (s *Scanner) shouldSkipDir(realRoot, path, name, absOut string, matcher *ignore.GitIgnore) bool {
	if slices.Contains(defaultIgnoredDirs, name) {
		return true
	}
	if rel, ok := relForMatch(realRoot, path); ok && slices.Contains(defaultIgnoredRelPaths, rel) {
		return true
	}
	if absOut != "" {
		// filepath.Abs (not EvalSymlinks) is sufficient here: WalkDir starts from
		// realRoot, which was already EvalSymlinks-resolved in Scan, so every walk
		// path is a real path under a real root — there is no unresolved symlink
		// component left to canonicalize for this dir-pruning comparison.
		if abs, err := filepath.Abs(path); err == nil && (abs == absOut || underRoot(absOut, abs)) {
			return true
		}
	}
	if matcher != nil {
		if rel, ok := relForMatch(realRoot, path); ok && matcher.MatchesPath(rel+"/") {
			return true
		}
	}
	return false
}

// symlinkNotice builds the appropriate notice for a skipped symlink, noting
// whether its target escapes the root (informational only — it is skipped
// either way under the no-follow policy).
func (s *Scanner) symlinkNotice(realRoot, path string) application.Notice {
	detail := "symlink not followed (no-follow policy)"
	if target, err := filepath.EvalSymlinks(path); err == nil && !underRoot(realRoot, target) {
		detail = fmt.Sprintf("symlink not followed; target %q escapes the scan root", target)
	}
	return application.Notice{
		Kind:   application.NoticeSkippedSymlink,
		Path:   path,
		Detail: detail,
	}
}

// relForMatch returns the slash-separated path of p relative to root, for
// gitignore matching. The second result is false if p is not under root.
func relForMatch(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if identity.EscapesRoot(rel) {
		return "", false
	}
	return rel, true
}

// underRoot reports whether path is the root or lies beneath it, using cleaned
// absolute paths and a separator-aware prefix check (so "/a/bc" is not under
// "/a/b").
func underRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
