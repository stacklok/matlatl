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

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/identity"
)

// Default resource caps (ADR 0003). They are safe-by-default and overridable
// via Config.
const (
	// DefaultMaxFileSizeBytes is the per-file size cap; larger files are skipped.
	DefaultMaxFileSizeBytes int64 = 10 << 20 // 10 MiB
	// DefaultMaxFiles is the discovery cap; discovery stops once it is reached.
	DefaultMaxFiles = 10_000
	// ignoreFileName is the per-root ignore file (gitignore semantics).
	ignoreFileName = ".doctopusignore"
)

// defaultIgnoredDirs are directory names skipped wholesale during the walk.
var defaultIgnoredDirs = []string{".git", "node_modules", "vendor"}

// markdownExts are the recognized markdown extensions (matched case-insensitively).
var markdownExts = []string{".md", ".markdown"}

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

		// No symlink following (ADR 0003): a symlinked entry is skipped and noticed.
		if d.Type()&fs.ModeSymlink != 0 {
			notices = append(notices, s.symlinkNotice(realRoot, path))
			return nil
		}

		if !isMarkdown(d.Name()) {
			return nil
		}

		// Respect ignore rules for files too.
		if rel, ok := relForMatch(realRoot, path); ok && matcher != nil && matcher.MatchesPath(rel) {
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

		// Stat the canonical (real) path, not the walk path: closes the
		// EvalSymlinks→read TOCTOU window by working from the resolved file from
		// here on. We also store realPath in ScannedFile.Path so the parser
		// reads exactly what we validated.
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

// loadIgnore compiles root/.doctopusignore if present, returning nil when
// absent or unreadable (a missing ignore file is not an error).
// CompileIgnoreFile handles line endings (including CRLF) itself, avoiding the
// trailing-\r bug of a manual ReadFile+Split.
func (s *Scanner) loadIgnore(realRoot string) *ignore.GitIgnore {
	p := filepath.Join(realRoot, ignoreFileName)
	matcher, err := ignore.CompileIgnoreFile(p)
	if err != nil {
		return nil
	}
	return matcher
}

// shouldSkipDir reports whether a directory should be pruned from the walk.
func (s *Scanner) shouldSkipDir(realRoot, path, name, absOut string, matcher *ignore.GitIgnore) bool {
	if slices.Contains(defaultIgnoredDirs, name) {
		return true
	}
	if absOut != "" {
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

// isMarkdown reports whether name has a recognized markdown extension
// (case-insensitive).
func isMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return slices.Contains(markdownExts, ext)
}

// relForMatch returns the slash-separated path of p relative to root, for
// gitignore matching. The second result is false if p is not under root.
func relForMatch(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
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
